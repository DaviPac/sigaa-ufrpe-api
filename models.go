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

type ArquivoCronograma struct {
	Nome  string `json:"nome"`
	Chave string `json:"chave"` // Ex: formAva:j_id_jsp_1879301362_314:2...
	ID    string `json:"id"`    // Ex: 78BAF65DEA73ACA64CCC34BC50B6916D82289CDC
}

type CronogramaItem struct {
	Titulo   string              `json:"titulo"`
	Conteudo string              `json:"conteudo"`
	Arquivos []ArquivoCronograma `json:"arquivos"`
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
	Local      string           `json:"local"`
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

type TurmaAtestado struct {
	Codigo    string `json:"codigo"`
	Nome      string `json:"nome"`
	Professor string `json:"professor"`
	Local     string `json:"local"`
	Tipo      string `json:"tipo"`
	Status    string `json:"status"`
	Horario   string `json:"horario"`
}

type AtestadoMatricula struct {
	PeriodoLetivo     string          `json:"periodoLetivo"`
	Matricula         string          `json:"matricula"`
	Vinculo           string          `json:"vinculo"`
	Nome              string          `json:"nome"`
	Nivel             string          `json:"nivel"`
	Curso             string          `json:"curso"`
	Turmas            []TurmaAtestado `json:"turmas"`
	CodigoVerificacao string          `json:"codigoVerificacao"`
}

type ComponenteCurricular struct {
	Codigo       string `json:"codigo"`
	Nome         string `json:"nome"`
	CargaHoraria string `json:"cargaHoraria"`
	Tipo         string `json:"tipo"`  // Obrigatória, Optativa, Complementar
	Nivel        string `json:"nivel"` // Ex: "1", "2", "optativas", "complementares"
}

type EstruturaCurricular struct {
	Codigo                  string                 `json:"codigo"`
	MatrizCurricular        string                 `json:"matrizCurricular"`
	PeriodoVigor            string                 `json:"periodoVigor"`
	CargaHorariaTotalMin    string                 `json:"cargaHorariaTotalMin"`
	CargaHorariaOptativaMin string                 `json:"cargaHorariaOptativaMin"`
	CargaHorariaObrigatoria string                 `json:"cargaHorariaObrigatoria"`
	PrazoMinimoSemestres    string                 `json:"prazoMinimoSemestres"`
	PrazoMedioSemestres     string                 `json:"prazoMedioSemestres"`
	PrazoMaximoSemestres    string                 `json:"prazoMaximoSemestres"`
	Componentes             []ComponenteCurricular `json:"componentes"`
}
