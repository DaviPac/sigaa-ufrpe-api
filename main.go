package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/gin-gonic/gin"

	_ "sigaaApi/docs"

	"github.com/gin-contrib/cors"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// @title SIGAA API
// @version 1.0
// @description API que interage com o SIGAA (login, turmas, etc).
// @host localhost:8080
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Use "Bearer {jsessionid}"
func main() {
	router := gin.Default()
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"https://conecta-ufrpe.vercel.app", "http://localhost:4200"},
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	router.GET("/calendario", handleGetCalendario)
	router.GET("/calendario/url", handleGetCalendarioURL)

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	api := router.Group("/")
	api.Use(AuthMiddleware())
	{
		api.GET("/main-data", handleGetMainData)
		api.POST("/notas", handlePostNotas)
		api.POST("/turma", handlePostTurma)
	}

	router.POST("/login", handleLogin)

	log.Println("🚀 Servidor rodando em http://localhost:8080")
	router.Run(":8080")
}

// @Summary Faz login no SIGAA
// @Tags Auth
// @Accept json
// @Produce json
// @Param credentials body LoginRequest true "Credenciais do usuário"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /login [post]
func handleLogin(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON inválido"})
		return
	}

	jsessionid, err := repeatLoginReq(req.Username, req.Password, 0)
	if err != nil {
		fmt.Println(err)
		if errors.Is(err, ErrInvalidCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": fmt.Sprintf("Falha no login: %s", err)})
		} else {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Falha ao se comunicar com o SIGAA. Tente novamente mais tarde."})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"jsessionid": jsessionid})
}

func repeatLoginReq(username string, password string, count int) (string, error) {
	jsessionid, err := Login(username, password)
	if err != nil {
		fmt.Println(err)
		if errors.Is(err, ErrInvalidCredentials) {
			return "", ErrInvalidCredentials
		} else {
			if count >= 5 {
				return "", err
			}
			return repeatLoginReq(username, password, count+1)
		}
	}
	return jsessionid, nil
}

// @Summary Retorna dados principais (nome e turmas)
// @Tags SIGAA
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Router /main-data [get]
// @Security BearerAuth
func handleGetMainData(c *gin.Context) {
	jsessionid := c.GetString("jsessionid")

	nome, ch, indices, avaliacoes, turmas, newJsessionid, viewState, err := GetMainData(jsessionid)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Sessão expirada ou inválida"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"nome":         nome,
		"turmas":       turmas,
		"avaliacoes":   avaliacoes,
		"indices":      indices,
		"cargaHoraria": ch,
		"jsessionid":   newJsessionid,
		"viewState":    viewState,
	})
}

type TurmaPostRequest struct {
	Turma     TurmaData `json:"turma" binding:"required"`
	ViewState string    `json:"viewState" binding:"required"`
}

// @Summary Retorna dados detalhados de uma turma (POST)
// @Tags SIGAA
// @Accept json
// @Produce json
// @Param body body TurmaPostRequest true "Turma e viewState"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /turma [post]
// @Security BearerAuth
func handlePostTurma(c *gin.Context) {
	var req TurmaPostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON inválido: " + err.Error()})
		return
	}

	jsessionid := c.GetString("jsessionid")
	turmaAtualizada, newJsessionid, newViewState, err := GetTurmaData(req.Turma, jsessionid, req.ViewState)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar dados da turma: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"turma":      turmaAtualizada,
		"jsessionid": newJsessionid,
		"viewState":  newViewState,
	})
}

type NotasRequest struct {
	ViewState string `json:"viewState" binding:"required"`
}

// @Summary Baixa o HTML contendo notas
// @Tags SIGAA
// @Accept json
// @Produce json
// @Param body body NotasRequest true "ViewState atual"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /notas [post]
// @Security BearerAuth
func handlePostNotas(c *gin.Context) {
	var req NotasRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON inválido"})
		return
	}

	jsessionid := c.GetString("jsessionid")

	// Chama sua função real
	notas, newJsessionid, newViewState, err := GetNotas(jsessionid, req.ViewState)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Sessão expirada ou inválida"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "HTML de notas baixado com sucesso!",
		"jsessionid": newJsessionid,
		"viewState":  newViewState,
		"notas":      notas,
	})
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token ausente ou inválido"})
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		c.Set("jsessionid", token)
		c.Next()
	}
}

func handleGetCalendarioURL(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "*")

	if c.Request.Method == http.MethodOptions {
		c.Status(http.StatusOK)
		return
	}
	client := &http.Client{}
	req, _ := http.NewRequest("GET", "https://preg.ufrpe.br/br/calendario-academico", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	res, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao acessar a página"})
		return
	}
	defer res.Body.Close()

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao processar HTML"})
		return
	}

	selection := doc.Find(".field-items > .field-item.even")
	url, exists := selection.Last().Children().Last().Find("a").Attr("href")
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "PDF não encontrado"})
		return
	}

	if !strings.HasPrefix(url, "http") {
		url = "https://preg.ufrpe.br" + url
	}

	c.JSON(http.StatusOK, gin.H{
		"url": url,
	})
}

func handleGetCalendario(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "*")

	if c.Request.Method == http.MethodOptions {
		c.Status(http.StatusOK)
		return
	}

	client := &http.Client{}
	req, _ := http.NewRequest("GET", "https://preg.ufrpe.br/br/calendario-academico", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	res, err := client.Do(req)
	if err != nil {
		c.String(http.StatusInternalServerError, "Erro ao acessar o PDF: %v", err)
		return
	}
	defer res.Body.Close()
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		c.String(http.StatusInternalServerError, "Erro ao acessar o PDF: %v", err)
		return
	}

	selection := doc.Find(".field-items > .field-item.even")
	url, exists := selection.Last().Children().Last().Find("a").Attr("href")
	if !exists {
		c.String(http.StatusNotFound, "Não foi possível encontrar o PDF")
		return
	}

	if strings.HasPrefix(url, "/") {
		url = "https://preg.ufrpe.br" + url
	}

	// Faz a requisição HTTP
	resp, err := http.Get(url)
	if err != nil {
		c.String(http.StatusInternalServerError, "Erro ao acessar o PDF: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.String(http.StatusBadGateway, "Servidor remoto retornou status %d", resp.StatusCode)
		return
	}

	// Define cabeçalhos de resposta
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", "inline; filename=calendario_2025.pdf")

	// Copia o conteúdo do PDF diretamente para a resposta HTTP
	io.Copy(c.Writer, resp.Body)
}
