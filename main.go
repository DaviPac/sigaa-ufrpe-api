package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
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
	// O banco só é usado pelas rotas /classroom. Se ele estiver indisponível,
	// o servidor sobe do mesmo jeito e só essas rotas ficam degradadas.
	InitDB(os.Getenv("DATABASE_URL"))
	defer CloseDB()

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
		api.POST("/componente", handlePostComponente)
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

	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	log.Printf("🚀 Servidor rodando em http://localhost%s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("servidor encerrado: %v", err)
	}
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
	if req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username e password são obrigatórios"})
		return
	}

	jsessionid, err := loginWithRetry(req.Username, req.Password, 5)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário ou senha inválidos"})
			return
		}
		log.Printf("login: falha ao comunicar com o SIGAA: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Falha ao se comunicar com o SIGAA. Tente novamente mais tarde."})
		return
	}

	c.JSON(http.StatusOK, gin.H{"jsessionid": jsessionid})
}

// loginWithRetry tenta o login algumas vezes em caso de erro transitório
// (rede/SIGAA fora), mas aborta imediatamente se as credenciais forem inválidas.
func loginWithRetry(username, password string, attempts int) (string, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		jsessionid, err := Login(username, password)
		if err == nil {
			return jsessionid, nil
		}
		if errors.Is(err, ErrInvalidCredentials) {
			return "", ErrInvalidCredentials
		}
		lastErr = err
		log.Printf("login: tentativa %d/%d falhou: %v", i+1, attempts, err)
		time.Sleep(time.Duration(i+1) * 300 * time.Millisecond)
	}
	return "", lastErr
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
	notas, anteriores, newJsessionid, newViewState, err := GetNotas(jsessionid, req.ViewState)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Sessão expirada ou inválida"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "HTML de notas baixado com sucesso!",
		"jsessionid": newJsessionid,
		"viewState":  newViewState,
		"notas":      notas,
		"anteriores": anteriores,
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

	// 3. Loop sequencial de raspagem. Uma turma que falha não derruba o
	// stream inteiro: emitimos o erro dela e re-sincronizamos o estado
	// (jsessionid/viewState) a partir do portal antes de seguir.
	for _, turmaBasica := range turmasBasicas {
		// Se o cliente fechou a conexão, paramos de raspar o SIGAA à toa.
		if c.Request.Context().Err() != nil {
			log.Println("Cliente desconectou antes do fim do stream")
			return
		}

		turmaDetalhada, nextJsessionid, nextViewState, err := GetTurmaData(turmaBasica, currentJsessionid, currentViewState)
		if err != nil {
			c.SSEvent("error", gin.H{
				"turma": turmaBasica.Nome,
				"error": "Falha ao ler turma: " + err.Error(),
			})
			c.Writer.Flush()

			// Se a sessão expirou de vez, não adianta continuar.
			if errors.Is(err, ErrSessaoExpirada) || errors.Is(err, ErrInvalidCredentials) {
				return
			}

			// Recupera um estado limpo a partir do portal para as próximas turmas.
			if _, _, _, _, _, _, reJsession, reViewState, reErr := GetMainData(currentJsessionid); reErr == nil {
				currentJsessionid = reJsession
				currentViewState = reViewState
			} else {
				// Sem conseguir re-sincronizar, encerramos.
				c.SSEvent("error", gin.H{"error": "Não foi possível re-sincronizar a sessão: " + reErr.Error()})
				c.Writer.Flush()
				return
			}
			continue
		}

		currentJsessionid = nextJsessionid
		currentViewState = nextViewState

		c.SSEvent("turma", turmaDetalhada)
		c.Writer.Flush()
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
		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token vazio"})
			return
		}
		// Aceita tanto "abc123" quanto "JSESSIONID=abc123"; normaliza para
		// o formato de cookie que as funções do SIGAA esperam.
		if !strings.Contains(token, "=") {
			token = "JSESSIONID=" + token
		}
		c.Set("jsessionid", token)
		c.Next()
	}
}

const calendarioPageURL = "https://preg.ufrpe.br/br/calendario-academico"

// resolveCalendarioPDFURL raspa a página da PREG e devolve a URL absoluta
// do PDF do calendário acadêmico.
func resolveCalendarioPDFURL() (string, error) {
	req, err := http.NewRequest(http.MethodGet, calendarioPageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", USER_AGENT)

	res, err := externalHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("erro ao acessar a página da PREG: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("PREG retornou status %d", res.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return "", fmt.Errorf("erro ao processar HTML da PREG: %w", err)
	}

	selection := doc.Find(".field-items > .field-item.even")
	href, exists := selection.Last().Children().Last().Find("a").Attr("href")
	if !exists || href == "" {
		return "", errors.New("link do PDF não encontrado na página da PREG")
	}

	if strings.HasPrefix(href, "/") {
		href = "https://preg.ufrpe.br" + href
	} else if !strings.HasPrefix(href, "http") {
		href = "https://preg.ufrpe.br/" + href
	}
	return href, nil
}

// O calendário é um recurso público e sem credenciais; liberamos para
// qualquer origem (o middleware global de CORS só cobre a allowlist).
func publicCORS(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "*")
}

func handleGetCalendarioURL(c *gin.Context) {
	publicCORS(c)
	if c.Request.Method == http.MethodOptions {
		c.Status(http.StatusOK)
		return
	}
	url, err := resolveCalendarioPDFURL()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": url})
}

func handleGetCalendario(c *gin.Context) {
	publicCORS(c)
	if c.Request.Method == http.MethodOptions {
		c.Status(http.StatusOK)
		return
	}
	url, err := resolveCalendarioPDFURL()
	if err != nil {
		c.String(http.StatusBadGateway, "%v", err)
		return
	}

	resp, err := externalHTTPClient.Get(url)
	if err != nil {
		c.String(http.StatusBadGateway, "Erro ao acessar o PDF: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.String(http.StatusBadGateway, "Servidor remoto retornou status %d", resp.StatusCode)
		return
	}

	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", "inline; filename=calendario.pdf")
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
		log.Printf("preparar arquivo: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Erro ao baixar arquivo do SIGAA: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	// Lê o arquivo todo (limitado a 50 MB para não estourar a RAM do servidor).
	fileBytes, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil {
		log.Printf("preparar arquivo: erro ao ler corpo: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Erro ao ler arquivo do SIGAA"})
		return
	}

	// Pega o nome do arquivo do header do Sigaa
	filename := "arquivo_sigaa"
	cd := resp.Header.Get("Content-Disposition")
	if _, params, e := mime.ParseMediaType(cd); e == nil && params["filename"] != "" {
		filename = params["filename"]
	} else if strings.Contains(cd, "filename=") {
		parts := strings.Split(cd, "filename=")
		filename = strings.Trim(strings.Split(parts[1], ";")[0], `"`)
	}

	ticket := uuid.New().String()

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
	estruturaCurricular, newJsessionid, viewState, err := getCurriculo(jsessionid)
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

type ComponenteRequest struct {
	IdComponente string `json:"idComponente" binding:"required"`
	Curriculo    string `json:"curriculo" binding:"required"`
	ViewState    string `json:"viewState" binding:"required"`
}

func handlePostComponente(c *gin.Context) {
	var req ComponenteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON inválido: " + err.Error()})
		return
	}
	jsessionid := c.GetString("jsessionid")
	componente, newJsessionid, viewState, err := getDetalhesComponente(jsessionid, req.ViewState, req.IdComponente, req.Curriculo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao acessar detalhes do componente: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"componente": componente,
		"jsessionid": newJsessionid,
		"viewState":  viewState,
	})
}
