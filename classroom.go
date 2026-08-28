package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/classroom/v1"
	"google.golang.org/api/option"
)

// CourseResponse é a estrutura simplificada que enviaremos para o seu frontend
type CourseResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Room string `json:"room,omitempty"`
}

// AssignmentResponse representa uma atividade/tarefa
type AssignmentResponse struct {
	Title         string `json:"title"`
	Description   string `json:"description,omitempty"`
	DueDate       string `json:"due_date,omitempty"`
	AlternateLink string `json:"alternateLink,omitempty"`
}

type AnnouncementResponse struct {
	ID            string `json:"id"`
	Text          string `json:"text"`
	CreationTime  string `json:"creationTime"`
	AlternateLink string `json:"alternateLink,omitempty"`
}

// TopicResponse representa a interface ClassroomTopic do frontend
type TopicResponse struct {
	TopicID string `json:"topicId"`
	Name    string `json:"name"`
}

type MaterialResponse struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Description   string `json:"description,omitempty"`
	AlternateLink string `json:"alternateLink,omitempty"`
	CreationTime  string `json:"creationTime,omitempty"`
}

// SubmissionResponse representa o status da atividade de um aluno (ex: entregue, pendente)
type SubmissionResponse struct {
	ID           string  `json:"id"`
	CourseWorkID string  `json:"courseWorkId"`
	State        string  `json:"state"` // Valores comuns: "NEW", "CREATED", "TURNED_IN", "RETURNED", "RECLAIMED_BY_STUDENT"
	Grade        float64 `json:"grade,omitempty"`
}

func getGoogleOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  "https://sigaa-ufrpe-api-production.up.railway.app/classroom/callback",
		Scopes: []string{
			"https://www.googleapis.com/auth/classroom.courses.readonly",
			"https://www.googleapis.com/auth/classroom.course-work.readonly",
			"https://www.googleapis.com/auth/classroom.student-submissions.me.readonly",
			"https://www.googleapis.com/auth/classroom.announcements.readonly",
			"https://www.googleapis.com/auth/classroom.courseworkmaterials.readonly",
			"https://www.googleapis.com/auth/classroom.topics.readonly",
		},
		Endpoint: google.Endpoint,
	}
}

func HandleGoogleAuthURL(c *gin.Context) {
	matricula := c.Query("matricula")
	if matricula == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Informe a matrícula na query (?matricula=123)"})
		return
	}

	config := getGoogleOAuthConfig()
	url := config.AuthCodeURL(matricula, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	c.JSON(http.StatusOK, gin.H{"auth_url": url})
}

func HandleGoogleCallback(c *gin.Context) {
	state := c.Query("state")
	code := c.Query("code")

	if state == "" || code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Requisição inválida do Google"})
		return
	}

	config := getGoogleOAuthConfig()
	token, err := config.Exchange(context.Background(), code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao trocar o código pelo token"})
		return
	}

	err = SaveToken(state, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao salvar token no banco"})
		return
	}

	c.Redirect(http.StatusFound, "https://sigaa-lite-ufrpe.vercel.app/turma?google_sync=success")
}

func getClient(ctx context.Context, matricula string) (*classroom.Service, error) {
	token, err := GetTokenFromDB(matricula)
	if err != nil {
		return nil, err
	}

	config := getGoogleOAuthConfig()
	client := config.Client(ctx, token)

	srv, err := classroom.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("erro ao criar serviço do classroom: %v", err)
	}

	return srv, nil
}

// courseReq é o corpo comum a quase todas as rotas de /classroom.
type courseReq struct {
	Matricula string `json:"matricula" binding:"required"`
	CourseID  string `json:"course_id"`
}

// classroomServiceFor lê o corpo, valida e devolve um serviço autenticado.
// Em qualquer falha ele já responde ao cliente e retorna ok=false, então o
// handler só precisa: srv, req, ok := classroomServiceFor(c); if !ok { return }
func classroomServiceFor(c *gin.Context, needCourseID bool) (*classroom.Service, courseReq, bool) {
	var req courseReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Corpo inválido. Esperado: matricula" + iff(needCourseID, " e course_id", "")})
		return nil, req, false
	}
	if needCourseID && req.CourseID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "course_id é obrigatório"})
		return nil, req, false
	}

	srv, err := getClient(c.Request.Context(), req.Matricula)
	if err != nil {
		switch {
		case errors.Is(err, ErrDBUnavailable):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Serviço de integração com o Google temporariamente indisponível"})
		default:
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Aluno não autenticado no Google", "details": err.Error()})
		}
		return nil, req, false
	}
	return srv, req, true
}

func iff(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// HandleListCourses busca as turmas
func HandleListCourses(c *gin.Context) {
	srv, _, ok := classroomServiceFor(c, false)
	if !ok {
		return
	}

	rSrv, err := srv.Courses.List().StudentId("me").CourseStates("ACTIVE").Do()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Erro ao buscar cursos na API do Google"})
		return
	}

	courses := []CourseResponse{}
	for _, crs := range rSrv.Courses {
		courses = append(courses, CourseResponse{
			ID:   crs.Id,
			Name: crs.Name,
			Room: crs.Room,
		})
	}

	c.JSON(http.StatusOK, courses)
}

func HandleListAssignments(c *gin.Context) {
	srv, req, ok := classroomServiceFor(c, true)
	if !ok {
		return
	}

	rSrv, err := srv.Courses.CourseWork.List(req.CourseID).Do()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Erro ao buscar atividades"})
		return
	}

	assignments := []AssignmentResponse{}
	for _, cw := range rSrv.CourseWork {
		dueDate := ""
		if cw.DueDate != nil {
			dueDate = fmt.Sprintf("%02d/%02d/%d", cw.DueDate.Day, cw.DueDate.Month, cw.DueDate.Year)
		}

		assignments = append(assignments, AssignmentResponse{
			Title:         cw.Title,
			Description:   cw.Description,
			DueDate:       dueDate,
			AlternateLink: cw.AlternateLink,
		})
	}

	c.JSON(http.StatusOK, assignments)
}

// HandleListTopics busca os tópicos de uma turma
func HandleListTopics(c *gin.Context) {
	srv, req, ok := classroomServiceFor(c, true)
	if !ok {
		return
	}

	rSrv, err := srv.Courses.Topics.List(req.CourseID).Do()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Erro ao buscar tópicos na API do Google"})
		return
	}

	// Inicializa como slice vazio para evitar retornar 'null' no JSON
	topics := []TopicResponse{}

	for _, t := range rSrv.Topic {
		topics = append(topics, TopicResponse{
			TopicID: t.TopicId,
			Name:    t.Name,
		})
	}

	c.JSON(http.StatusOK, topics)
}

// HandleListAnnouncements busca os anúncios do mural de uma turma
func HandleListAnnouncements(c *gin.Context) {
	srv, req, ok := classroomServiceFor(c, true)
	if !ok {
		return
	}

	rSrv, err := srv.Courses.Announcements.List(req.CourseID).Do()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Erro ao buscar anúncios na API do Google"})
		return
	}

	// Inicializa como slice vazio para evitar retornar 'null' no JSON
	announcements := []AnnouncementResponse{}

	for _, a := range rSrv.Announcements {
		announcements = append(announcements, AnnouncementResponse{
			ID:            a.Id,
			Text:          a.Text,
			CreationTime:  a.CreationTime,
			AlternateLink: a.AlternateLink,
		})
	}

	c.JSON(http.StatusOK, announcements)
}

// HandleListMaterials busca os materiais de estudo de uma turma
func HandleListMaterials(c *gin.Context) {
	srv, req, ok := classroomServiceFor(c, true)
	if !ok {
		return
	}

	rSrv, err := srv.Courses.CourseWorkMaterials.List(req.CourseID).Do()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Erro ao buscar materiais na API do Google"})
		return
	}

	// Inicializa como slice vazio
	materials := []MaterialResponse{}

	for _, m := range rSrv.CourseWorkMaterial {
		materials = append(materials, MaterialResponse{
			ID:            m.Id,
			Title:         m.Title,
			Description:   m.Description,
			AlternateLink: m.AlternateLink,
			CreationTime:  m.CreationTime,
		})
	}

	c.JSON(http.StatusOK, materials)
}

// HandleListSubmissions busca as submissões do próprio aluno nas atividades de uma turma
func HandleListSubmissions(c *gin.Context) {
	srv, req, ok := classroomServiceFor(c, true)
	if !ok {
		return
	}

	// O ID de atividade "-" diz à API para trazer de TODAS as atividades da turma.
	// O UserId("me") garante que estamos apenas vendo a situação do usuário autenticado.
	rSrv, err := srv.Courses.CourseWork.StudentSubmissions.List(req.CourseID, "-").UserId("me").Do()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Erro ao buscar submissões na API do Google"})
		return
	}

	// Inicializa como slice vazio
	submissions := []SubmissionResponse{}

	for _, s := range rSrv.StudentSubmissions {
		submissions = append(submissions, SubmissionResponse{
			ID:           s.Id,
			CourseWorkID: s.CourseWorkId,
			State:        s.State,
			Grade:        s.AssignedGrade,
		})
	}

	c.JSON(http.StatusOK, submissions)
}
