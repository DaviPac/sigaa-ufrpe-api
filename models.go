package main

import "errors"

var ErrInvalidCredentials = errors.New("usuário ou senha inválidos")

const (
	FALTAS_INDEFINIDAS   = -2
	PRESENCA_NAO_LANCADA = -1
)

type IndicesAcademicos struct {
	MC    string `json:"mc"`
	IRA   string `json:"ira"`
	MCN   string `json:"mcn"`
	IECH  string `json:"iech"`
	IEPL  string `json:"iepl"`
	IEA   string `json:"iea"`
	IEAN  string `json:"iean"`
	IECHP string `json:"iechp"`
}

type CargasHorarias struct {
	OptativaPendente     string `json:"optativaPendente"`
	ObrigatoriaPendente  string `json:"obrigatoriaPendente"`
	ComplementarPendente string `json:"complementarPendente"`
	TotalCurriculo       string `json:"totalCurriculo"`
}

type Noticia struct {
	Titulo   string   `json:"titulo"`
	Conteudo []string `json:"conteudo"`
}

type CronogramaItem struct {
	Titulo   string `json:"titulo"`
	Conteudo string `json:"conteudo"`
}

type DisciplinaNotas struct {
	Codigo    string            `json:"codigo"`
	Nome      string            `json:"nome"`
	Notas     map[string]string `json:"notas"`
	Resultado string            `json:"resultado"`
	Faltas    string            `json:"faltas"`
	Situacao  string            `json:"situacao"`
}

type TurmaInfo struct {
	Nome        string `json:"nome"`
	FrontEndId  string `json:"frontEndId"`
	FormName    string `json:"formName"`
	ComponentId string `json:"componentId"`
}

type TurmaData struct {
	Nome       string           `json:"nome"`
	Horarios   []string         `json:"horarios"`
	Notas      DisciplinaNotas  `json:"notas"`
	Faltas     int              `json:"faltas"`
	Info       TurmaInfo        `json:"info"`
	Noticia    Noticia          `json:"noticia"`
	Cronograma []CronogramaItem `json:"cronograma"`
}

type Avaliacao struct {
	Nome      string `json:"nome"`
	TurmaNome string `json:"turmaNome"`
	Data      string `json:"data"`
	Tipo      string `json:"tipo"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Nome   string      `json:"nome"`
	Turmas []TurmaData `json:"turmas"`
}
