package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

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
		AllowOrigins:     []string{"https://conecta-ufrpe.vercel.app"}, // seu frontend
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	router.GET("/calendario", handleGetCalendario)

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

	log.Println("🤖 Tentando login no SIGAA...")
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

func handleGetCalendario(c *gin.Context) {
	url := "https://preg.ufrpe.br/sites/ww4.depaacademicos.ufrpe.br/files/Calendario%20cursos%20presenciais%202025%20final%20%282%29.pdf"

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
