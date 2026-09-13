package main

import (
	"os"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func loadFixtureDoc(t *testing.T, path string) *goquery.Document {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("erro ao abrir fixture %s: %v", path, err)
	}
	defer f.Close()

	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		t.Fatalf("erro ao parsear fixture %s: %v", path, err)
	}
	return doc
}

func TestParseUnidadesBusca(t *testing.T) {
	doc := loadFixtureDoc(t, "testdata/turmas_busca.html")
	unidades := parseUnidadesBusca(doc)

	if len(unidades) != 2 {
		t.Fatalf("esperava 2 unidades, obteve %d: %+v", len(unidades), unidades)
	}
	if unidades[0].Codigo != "530" || unidades[0].Nome != "DEPARTAMENTO DE COMPUTAÇÃO-DC - RECIFE" {
		t.Errorf("unidade[0] inesperada: %+v", unidades[0])
	}
	if unidades[1].Codigo != "517" || unidades[1].Nome != "DEPARTAMENTO DE ECONOMIA-DECON - RECIFE" {
		t.Errorf("unidade[1] inesperada: %+v", unidades[1])
	}
}

func TestParseTurmasOfertadas(t *testing.T) {
	doc := loadFixtureDoc(t, "testdata/turmas_busca.html")
	componentes := parseTurmasOfertadas(doc)

	if len(componentes) != 2 {
		t.Fatalf("esperava 2 componentes, obteve %d: %+v", len(componentes), componentes)
	}

	primeiro := componentes[0]
	if primeiro.Codigo != "06209" || primeiro.Nome != "INTRODUÇÃO À COMPUTAÇÃO" || primeiro.IdComponentePublico != "14140" {
		t.Errorf("componente[0] inesperado: %+v", primeiro)
	}
	if len(primeiro.Turmas) != 1 {
		t.Fatalf("esperava 1 turma em componente[0], obteve %d", len(primeiro.Turmas))
	}
	turma := primeiro.Turmas[0]
	if turma.Turma != "01" || turma.AnoPeriodo != "2026.2" || turma.Docente != "JOÃO DA SILVA (60h)" || turma.Local != "SALA 10" {
		t.Errorf("turma inesperada em componente[0]: %+v", turma)
	}

	segundo := componentes[1]
	if segundo.Codigo != "14717" || segundo.Nome != "GESTÃO DE PROCESSOS DE NEGÓCIO" || segundo.IdComponentePublico != "18223" {
		t.Errorf("componente[1] inesperado: %+v", segundo)
	}
	if len(segundo.Turmas) != 1 || segundo.Turmas[0].Docente != "FRANCIELLE SILVA DOS SANTOS (60h)" {
		t.Errorf("turmas inesperadas em componente[1]: %+v", segundo.Turmas)
	}
}
