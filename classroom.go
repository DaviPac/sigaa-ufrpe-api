package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/classroom/v1"
	"google.golang.org/api/option"
)

// RequestBody representa o JSON esperado na requisição
type RequestBody struct {
	Matricula string `json:"matricula"`
}

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

// ListCoursesHandler retorna a lista de turmas que o aluno está inscrito
func ListCoursesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido. Use POST.", http.StatusMethodNotAllowed)
		return
	}

	var req RequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Erro ao ler o body", http.StatusBadRequest)
		return
	}

	srv, err := getClient(r.Context(), req.Matricula)
	if err != nil {
		http.Error(w, "Não autorizado ou aluno não autenticado no Google", http.StatusUnauthorized)
		return
	}

	// Puxa os cursos do aluno (status ACTIVE)
	// studentId = "me" pega os dados do dono do token
	rSrv, err := srv.Courses.List().StudentId("me").CourseStates("ACTIVE").Do()
	if err != nil {
		http.Error(w, fmt.Sprintf("Erro ao buscar cursos: %v", err), http.StatusInternalServerError)
		return
	}

	var courses []CourseResponse
	for _, c := range rSrv.Courses {
		courses = append(courses, CourseResponse{
			ID:   c.Id,
			Name: c.Name,
			Room: c.Room,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(courses)
}

// ListAssignmentsHandler retorna as atividades de uma turma específica
func ListAssignmentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido. Use POST.", http.StatusMethodNotAllowed)
		return
	}

	// Precisamos da matrícula e também do ID do curso
	type AssignmentReq struct {
		Matricula string `json:"matricula"`
		CourseID  string `json:"course_id"`
	}

	var req AssignmentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Erro ao ler o body", http.StatusBadRequest)
		return
	}

	srv, err := getClient(r.Context(), req.Matricula)
	if err != nil {
		http.Error(w, "Não autorizado", http.StatusUnauthorized)
		return
	}

	// Puxa as atividades (courseWork) daquele curso específico
	rSrv, err := srv.Courses.CourseWork.List(req.CourseID).Do()
	if err != nil {
		http.Error(w, fmt.Sprintf("Erro ao buscar atividades: %v", err), http.StatusInternalServerError)
		return
	}

	var assignments []AssignmentResponse
	for _, cw := range rSrv.CourseWork {
		dueDate := ""
		if cw.DueDate != nil {
			dueDate = fmt.Sprintf("%02d/%02d/%d", cw.DueDate.Day, cw.DueDate.Month, cw.DueDate.Year)
		}

		assignments = append(assignments, AssignmentResponse{
			Title:       cw.Title,
			Description: cw.Description,
			DueDate:     dueDate,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(assignments)
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
	// Passamos a matrícula no "state" para sabermos de quem é o token quando o Google retornar
	url := config.AuthCodeURL(matricula, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	c.JSON(http.StatusOK, gin.H{"auth_url": url})
}

func HandleGoogleCallback(c *gin.Context) {
	state := c.Query("state") // Esta é a matrícula que passamos antes
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

	// Salva no banco de dados (usando a função do db.go que criamos)
	err = SaveToken(state, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao salvar token no banco"})
		return
	}

	// Redireciona o aluno de volta para o frontend (ajuste a URL para o seu Vercel/Angular/React)
	c.Redirect(http.StatusFound, "http://localhost:4200/dashboard?google_sync=success")
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

// HandleListCourses busca as turmas
func HandleListCourses(c *gin.Context) {
	var req struct {
		Matricula string `json:"matricula"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Erro ao ler o body. Esperado: matricula"})
		return
	}

	srv, err := getClient(c.Request.Context(), req.Matricula)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Aluno não autenticado no Google", "details": err.Error()})
		return
	}

	rSrv, err := srv.Courses.List().StudentId("me").CourseStates("ACTIVE").Do()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar cursos na API do Google"})
		return
	}

	var courses []CourseResponse
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
	var req struct {
		Matricula string `json:"matricula"`
		CourseID  string `json:"course_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Erro ao ler body. Esperado: matricula e course_id"})
		return
	}

	srv, err := getClient(c.Request.Context(), req.Matricula)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Aluno não autenticado no Google"})
		return
	}

	rSrv, err := srv.Courses.CourseWork.List(req.CourseID).Do()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar atividades"})
		return
	}

	var assignments []AssignmentResponse
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
	var req struct {
		Matricula string `json:"matricula"`
		CourseID  string `json:"course_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Erro ao ler body. Esperado: matricula e course_id"})
		return
	}

	srv, err := getClient(c.Request.Context(), req.Matricula)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Aluno não autenticado no Google"})
		return
	}

	// Faz a requisição para listar os tópicos do curso
	rSrv, err := srv.Courses.Topics.List(req.CourseID).Do()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar tópicos na API do Google"})
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
	var req struct {
		Matricula string `json:"matricula"`
		CourseID  string `json:"course_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Erro ao ler body. Esperado: matricula e course_id"})
		return
	}

	srv, err := getClient(c.Request.Context(), req.Matricula)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Aluno não autenticado no Google"})
		return
	}

	// Faz a requisição para listar os anúncios do curso
	rSrv, err := srv.Courses.Announcements.List(req.CourseID).Do()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao buscar anúncios na API do Google"})
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
