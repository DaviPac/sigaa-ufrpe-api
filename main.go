package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

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
	/*if err := InitDB(); err != nil {
		log.Fatalf("Falha crítica ao iniciar o banco: %v", err)
	}*/
	defer DB.Close()
	router := gin.Default()
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"https://sigaa-lite-ufrpe.vercel.app", "https://conecta-ufrpe.vercel.app", "http://localhost:4200", "https://mozilla.github.io"},
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	router.GET("/calendario", handleGetCalendario)
	router.GET("/turma/arquivo/download", handleGetDownload)
	router.GET("/calendario/url", handleGetCalendarioURL)

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	api := router.Group("/")
	api.Use(AuthMiddleware())
	{
		api.GET("/main-data", handleGetMainData)
		api.POST("/notas", handlePostNotas)
		api.POST("/turma", handlePostTurma)
		api.POST("/matricula", handlePostMatricula)
		api.POST("/historico", handlePostHistorico)
		api.POST("/vinculo", handlePostVinculo)
		//api.POST("/download", handlePostDownloadArquivo)
		api.POST("/turma/arquivo/preparar", handlePostPrepararArquivo)
		api.GET("/turmas-stream", handleGetTurmasStream)
		api.GET("/curriculo", handleGetCurriculo)
	}

	router.POST("/login", handleLogin)

	classroomAPI := router.Group("/classroom")
	{
		// 1. Gera a URL para o aluno logar no Google
		classroomAPI.GET("/auth-url", HandleGoogleAuthURL)

		// 2. Rota de retorno do Google (salva o token no banco)
		classroomAPI.GET("/callback", HandleGoogleCallback)

		// 3. Busca turmas e atividades (Esperam JSON com {"matricula": "..."})
		classroomAPI.POST("/courses", HandleListCourses)
		classroomAPI.POST("/assignments", HandleListAssignments)
		classroomAPI.POST("/announcements", HandleListAnnouncements)
		classroomAPI.POST("/topics", HandleListTopics)
		classroomAPI.POST("/materials", HandleListMaterials)
		classroomAPI.POST("/submissions", HandleListSubmissions)
	}

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

	nome, matricula, ch, indices, avaliacoes, turmas, newJsessionid, viewState, err := GetMainData(jsessionid)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Sessão expirada ou inválida"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"nome":         nome,
		"matricula":    matricula,
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

// @Summary Retorna dados detalhados das turmas em tempo real (SSE)
// @Tags SIGAA
// @Produce text/event-stream
// @Router /turmas-stream [get]
// @Security BearerAuth
func handleGetTurmasStream(c *gin.Context) {
	jsessionid := c.GetString("jsessionid")

	// 1. Configurar cabeçalhos HTTP exigidos para uma conexão SSE
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")

	// 2. Busca inicial: Coleta as turmas básicas e o viewState inicial
	_, _, _, _, _, turmasBasicas, currentJsessionid, currentViewState, err := GetMainData(jsessionid)
	if err != nil {
		c.SSEvent("error", gin.H{"error": "Erro ao carregar dados iniciais: " + err.Error()})
		c.Writer.Flush()
		return
	}

	// (Opcional) Envia um evento avisando o frontend quantas turmas serão carregadas
	c.SSEvent("start", gin.H{"total": len(turmasBasicas)})
	c.Writer.Flush()

	// 3. Inicia o loop sequencial de raspagem
	for _, turmaBasica := range turmasBasicas {
		// Proteção importante: Verifica se o usuário fechou o PWA/cancelou a request
		// Isso evita que o backend continue raspando o SIGAA como um zumbi
		if c.Request.Context().Err() != nil {
			log.Println("Cliente desconectou antes do fim do stream")
			return
		}

		// Entra na turma, pega os detalhes e volta (estado atualizado)
		turmaDetalhada, nextJsessionid, nextViewState, err := GetTurmaData(turmaBasica, currentJsessionid, currentViewState)
		if err != nil {
			// Informa erro de uma turma específica
			c.SSEvent("error", gin.H{
				"turma": turmaBasica, // manda qual falhou pra facilitar o debug
				"error": "Falha ao ler turma: " + err.Error(),
			})
			c.Writer.Flush()

			// Aqui você decide: 'return' para parar tudo, ou 'continue' para ignorar e tentar a próxima turma.
			// Recomendo parar (return), pois o JSF do SIGAA provavelmente quebrou o viewState com o erro.
			return
		}

		// Atualiza as variáveis de estado para a próxima iteração do loop
		currentJsessionid = nextJsessionid
		currentViewState = nextViewState

		// Emite os dados da turma coletada!
		c.SSEvent("turma", turmaDetalhada)
		c.Writer.Flush() // O Flush() é obrigatório para forçar o envio imediato do "chunk"
	}

	// 4. Finaliza a transmissão enviando o viewState e jsessionid finais
	// O frontend pode guardar isso para futuras ações (ex: baixar um arquivo de uma aula)
	c.SSEvent("done", gin.H{
		"message":    "Todas as turmas foram carregadas",
		"jsessionid": currentJsessionid,
		"viewState":  currentViewState,
	})
	c.Writer.Flush()
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

type MatriculaRequest struct {
	ViewState string `json:"viewState" binding:"required"`
}

// @Summary Retorna dados estruturados do atestado de matrícula
// @Tags SIGAA
// @Accept json
// @Produce json
// @Param body body MatriculaRequest true "ViewState"
// @Success 200 {object} AtestadoMatricula
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /turma [post]
// @Security BearerAuth
func handlePostMatricula(c *gin.Context) {
	var req MatriculaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "JSON inválido: " + err.Error(),
		})
		return
	}

	jsessionid := c.GetString("jsessionid")

	html, _, err := GetAtestadoMatricula(req.ViewState, jsessionid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Erro ao buscar matrícula: " + err.Error(),
		})
		return
	}

	atestado, err := ParseAtestadoMatricula(html)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Erro ao parsear atestado: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, atestado)
}

type HistoricoRequest struct {
	ViewState string `json:"viewState" binding:"required"`
}

// @Summary Retorna o PDF do Histórico do aluno
// @Tags SIGAA
// @Accept json
// @Produce application/pdf
// @Param body body HistoricoRequest true "ViewState"
// @Success 200 {file} file "Histórico em PDF"
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /historico [post]
// @Security BearerAuth
func handlePostHistorico(c *gin.Context) {
	var req HistoricoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON inválido: " + err.Error()})
		return
	}

	jsessionid := c.GetString("jsessionid")

	// Chama a função que retorna o stream do PDF
	resp, err := FetchHistoricoPDF(req.ViewState, jsessionid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar histórico: " + err.Error()})
		return
	}
	// Garante que o corpo da resposta do SIGAA será fechado após o envio ao cliente
	defer resp.Body.Close()

	// Define os cabeçalhos para o navegador entender que é um PDF
	c.Header("Content-Type", "application/pdf")
	// Dica: Use "inline" para abrir no navegador, ou "attachment" para forçar o download
	c.Header("Content-Disposition", "attachment; filename=historico.pdf")

	// Faz o stream direto do SIGAA para o client do seu app (alta performance)
	io.Copy(c.Writer, resp.Body)
}

type VinculoRequest struct {
	ViewState string `json:"viewState" binding:"required"`
}

// @Summary Retorna o PDF do Vínculo do aluno
// @Tags SIGAA
// @Accept json
// @Produce application/pdf
// @Param body body VinculoRequest true "ViewState"
// @Success 200 {file} file "Vínculo em PDF"
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /vinculo [post]
// @Security BearerAuth
func handlePostVinculo(c *gin.Context) {
	var req VinculoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON inválido: " + err.Error()})
		return
	}

	jsessionid := c.GetString("jsessionid")

	// Chama a função que retorna o stream do PDF
	resp, err := FetchVinculoPDF(req.ViewState, jsessionid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar vínculo: " + err.Error()})
		return
	}
	// Garante que o corpo da resposta do SIGAA será fechado após o envio ao cliente
	defer resp.Body.Close()

	// Define os cabeçalhos para o navegador entender que é um PDF
	c.Header("Content-Type", "application/pdf")
	// Dica: Use "inline" para abrir no navegador, ou "attachment" para forçar o download
	c.Header("Content-Disposition", "attachment; filename=historico.pdf")

	// Faz o stream direto do SIGAA para o client do seu app (alta performance)
	io.Copy(c.Writer, resp.Body)
}

type DownloadArquivoRequest struct {
	ViewState string    `json:"viewState" binding:"required"`
	Chave     string    `json:"chave" binding:"required"`
	ID        string    `json:"id" binding:"required"`
	Turma     TurmaData `json:"turma" binding:"required"`
}

// @Summary Baixa um arquivo do cronograma da turma
// @Tags SIGAA
// @Accept json
// @Produce application/octet-stream
// @Param body body DownloadArquivoRequest true "Dados do Arquivo"
// @Success 200 {file} file "Arquivo baixado"
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /turma/arquivo [post]
// @Security BearerAuth
func handlePostDownloadArquivo(c *gin.Context) {
	var req DownloadArquivoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON inválido: " + err.Error()})
		return
	}

	jsessionid := c.GetString("jsessionid")

	// Chama a função que faz o POST pro Sigaa e retorna o stream e os novos estados
	resp, newJsessionid, newViewState, err := BaixarArquivoSigaa(jsessionid, req.ViewState, req.Chave, req.ID, req.Turma)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao baixar arquivo: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	// -------------------------------------------------------------------------
	// ADIÇÃO DOS ESTADOS NOS HEADERS
	// Como o body será o arquivo, enviamos os novos estados via headers HTTP
	// -------------------------------------------------------------------------
	c.Header("X-New-Jsessionid", newJsessionid)
	c.Header("X-New-Viewstate", newViewState)

	c.Header("Access-Control-Expose-Headers", "X-New-Jsessionid, X-New-Viewstate, Content-Disposition")

	// Repassa os headers originais do arquivo vindos do Sigaa para o cliente.
	for k, values := range resp.Header {
		for _, v := range values {
			c.Writer.Header().Add(k, v)
		}
	}

	// Define o status HTTP de sucesso (geralmente 200)
	c.Status(resp.StatusCode)

	// Faz o stream direto do Sigaa para o cliente da sua API
	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		fmt.Printf("Erro ao fazer stream do arquivo: %v\n", err)
	}
}

var downloadCache sync.Map

type CachedFile struct {
	Data        []byte
	ContentType string
	Filename    string
}

// 1. ROTA POST: Prepara o download
func handlePostPrepararArquivo(c *gin.Context) {
	var req DownloadArquivoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON inválido: " + err.Error()})
		return
	}

	jsessionid := c.GetString("jsessionid")

	// Faz a requisição pro SIGAA. Aqui o arquivo vem pra RAM do seu servidor Go.
	resp, newJsessionid, newViewState, err := BaixarArquivoSigaa(jsessionid, req.ViewState, req.Chave, req.ID, req.Turma)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro no Sigaa"})
		return
	}
	defer resp.Body.Close()

	// Lê o arquivo todo
	fileBytes, _ := io.ReadAll(resp.Body)

	// Pega o nome do arquivo do header do Sigaa
	filename := "arquivo_sigaa"
	cd := resp.Header.Get("Content-Disposition")
	if strings.Contains(cd, "filename=") {
		parts := strings.Split(cd, "filename=")
		filename = strings.ReplaceAll(strings.Split(parts[1], ";")[0], "\"", "")
	}

	// Gera um ticket único
	ticket := uuid.New().String() // ou gere uma string aleatória

	// Salva no cache
	downloadCache.Store(ticket, CachedFile{
		Data:        fileBytes,
		ContentType: resp.Header.Get("Content-Type"),
		Filename:    filename,
	})

	// Limpa o cache após 2 minutos (pra não vazar RAM se o usuário desistir)
	go func(t string) {
		time.Sleep(2 * time.Minute)
		downloadCache.Delete(t)
	}(ticket)

	// Retorna SÓ os estados e o ticket pro Angular
	c.JSON(http.StatusOK, gin.H{
		"ticket":        ticket,
		"newJsessionid": newJsessionid,
		"newViewState":  newViewState,
	})
}

// 2. ROTA GET: O celular chama essa rota nativamente
func handleGetDownload(c *gin.Context) {
	ticket := c.Query("ticket")
	if val, ok := downloadCache.Load(ticket); ok {
		file := val.(CachedFile)

		c.Header("Content-Disposition", `attachment; filename="`+file.Filename+`"`)
		c.Data(http.StatusOK, file.ContentType, file.Data)

		// Apaga da memória assim que o download começar
		downloadCache.Delete(ticket)
	} else {
		c.String(http.StatusNotFound, "Link expirado ou inválido")
	}
}

func handleGetCurriculo(c *gin.Context) {
	jsessionid := c.GetString("jsessionid")
	estruturaCurricular, newJsessionid, viewState, err := getPaginaCurriculo(jsessionid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao acessar currículo: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"estruturaCurricular": estruturaCurricular,
		"jsessionid":          newJsessionid,
		"viewState":           viewState,
	})
}
