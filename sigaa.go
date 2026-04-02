package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const (
	URL_VIEW_LOGIN         = "https://sigs.ufrpe.br/sigaa/verTelaLogin.do"
	URL_PORTAL_DISCENTE    = "https://sigs.ufrpe.br/sigaa/portais/discente/discente.jsf"
	URL_FREQUENCIA         = "https://sigs.ufrpe.br/sigaa/ava/index.jsf"
	USER_AGENT             = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	URL_ATESTADO_MATRICULA = "https://sigs.ufrpe.br/sigaa/portais/discente/discente.jsf"
	URL_CURRICULO          = "https://sigs.ufrpe.br/sigaa/public/curso/curriculo.jsf"
)

var (
	reJSFChave = regexp.MustCompile(`'(formAva:[^']+)'\s*:`)
	reJSFId    = regexp.MustCompile(`'id'\s*:\s*'([^']+)'`)
)

func doSigaaRequest(method string, url string, jsessionid string, referer string, body io.Reader, contentType string) (*goquery.Document, string, error) {
	client := &http.Client{}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, jsessionid, fmt.Errorf("erro ao criar requisição: %w", err)
	}

	req.Header.Set("User-Agent", USER_AGENT)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if jsessionid != "" {
		req.Header.Set("Cookie", jsessionid)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, jsessionid, fmt.Errorf("erro ao fazer requisição para %s: %w", url, err)
	}
	fmt.Printf("URL: %v -- STATUS: %v\n", resp.Request.URL, resp.StatusCode)
	defer resp.Body.Close()

	newJsessionid := jsessionid
	cookieHeader := resp.Header.Get("Set-Cookie")
	if cookieHeader != "" {
		parts := strings.Split(cookieHeader, ";")
		if len(parts) > 0 {
			newJsessionid = parts[0]
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, newJsessionid, fmt.Errorf("status code inesperado %d para %s", resp.StatusCode, url)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, newJsessionid, fmt.Errorf("erro ao parsear HTML de %s: %w", url, err)
	}

	html, _ := doc.Html()
	if strings.Contains(html, "rio e/ou senha inv") {
		return nil, newJsessionid, ErrInvalidCredentials
	}
	if strings.Contains(html, "foi expirada") {
		return nil, newJsessionid, fmt.Errorf("sessão inválida ou expirada ao acessar %s", url)
	}

	return doc, newJsessionid, nil
}

func parseViewState(doc *goquery.Document, errorContext string) (string, error) {
	viewStateVal, exists := doc.Find("input[name='javax.faces.ViewState']").Attr("value")
	if !exists {
		return "", fmt.Errorf("não foi possível encontrar o javax.faces.ViewState na página: %s", errorContext)
	}
	return viewStateVal, nil
}

func FetchVinculoPDF(viewState string, jsessionid string) (*http.Response, error) {
	payload := url.Values{}
	payload.Set("menu:form_menu_discente", "menu:form_menu_discente")
	payload.Set("id", "107543")
	payload.Set("jscook_action", "menu_form_menu_discente_discente_menu:A]#{ declaracaoVinculo.emitirDeclaracao }")
	payload.Set("javax.faces.ViewState", viewState)

	client := &http.Client{}
	req, err := http.NewRequest("POST", URL_PORTAL_DISCENTE, strings.NewReader(payload.Encode()))
	if err != nil {
		return nil, fmt.Errorf("erro ao criar requisição de vinculo: %w", err)
	}

	// Adicionando os headers essenciais
	req.Header.Set("User-Agent", USER_AGENT)
	req.Header.Set("Referer", URL_PORTAL_DISCENTE)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", jsessionid)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao solicitar vinculo ao SIGAA: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("status code inesperado: %d", resp.StatusCode)
	}

	// Verificação de segurança: checa se o SIGAA devolveu mesmo um PDF
	// Se a sessão expirou, ele geralmente retorna um HTML (Content-Type: text/html)
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/pdf") {
		resp.Body.Close()
		return nil, errors.New("a resposta não é um PDF. A sessão pode ter expirado ou o ViewState é inválido")
	}

	// Retornamos o response inteiro (sem fechar o Body) para que o Handler faça o stream
	return resp, nil
}

func FetchHistoricoPDF(viewState string, jsessionid string) (*http.Response, error) {
	payload := url.Values{}
	payload.Set("menu:form_menu_discente", "menu:form_menu_discente")
	// Lembre-se: verifique se esse ID '107543' funciona para todos os alunos ou se precisará ser dinâmico
	payload.Set("id", "107543")
	payload.Set("jscook_action", "menu_form_menu_discente_discente_menu:A]#{ portalDiscente.historico }")
	payload.Set("javax.faces.ViewState", viewState)

	client := &http.Client{}
	req, err := http.NewRequest("POST", URL_PORTAL_DISCENTE, strings.NewReader(payload.Encode()))
	if err != nil {
		return nil, fmt.Errorf("erro ao criar requisição do histórico: %w", err)
	}

	// Adicionando os headers essenciais
	req.Header.Set("User-Agent", USER_AGENT)
	req.Header.Set("Referer", URL_PORTAL_DISCENTE)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", jsessionid)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao solicitar histórico ao SIGAA: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("status code inesperado: %d", resp.StatusCode)
	}

	// Verificação de segurança: checa se o SIGAA devolveu mesmo um PDF
	// Se a sessão expirou, ele geralmente retorna um HTML (Content-Type: text/html)
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/pdf") {
		resp.Body.Close()
		return nil, errors.New("a resposta não é um PDF. A sessão pode ter expirado ou o ViewState é inválido")
	}

	// Retornamos o response inteiro (sem fechar o Body) para que o Handler faça o stream
	return resp, nil
}

func GetAtestadoMatricula(viewState string, jsessionid string) (string, string, error) {
	payload := url.Values{}
	payload.Set("menu:form_menu_discente", "menu:form_menu_discente")
	payload.Set("id", "107543")
	payload.Set("jscook_action", "menu_form_menu_discente_discente_menu:A]#{ portalDiscente.atestadoMatricula }")
	payload.Set("javax.faces.ViewState", viewState)

	doc, newJsessionid, err := doSigaaRequest(
		"POST",
		URL_PORTAL_DISCENTE,
		jsessionid,
		URL_PORTAL_DISCENTE,
		strings.NewReader(payload.Encode()),
		"application/x-www-form-urlencoded",
	)
	if err != nil {
		return "", "", err
	}

	return doc.Text(), newJsessionid, nil
}

func ParseAtestadoMatricula(htmlContent string) (*AtestadoMatricula, error) {
	atestado := &AtestadoMatricula{}

	extract := func(pattern string) string {
		re := regexp.MustCompile(pattern)
		m := re.FindStringSubmatch(htmlContent)
		if len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
		return ""
	}

	// Período Letivo
	atestado.PeriodoLetivo = extract(`Letivo:\s*\n+\s*(\d{4}\.\d)`)

	// Nível
	atestado.Nivel = extract(`N[íi]vel:\s*\n?\s*([A-ZÇÃÕÁÉÍÓÚÂÊÎÔÛ ]+)`)

	// Matrícula
	atestado.Matricula = extract(`Matr[íi]cula:\s*\n?\s*(\d+)`)

	// Vínculo
	atestado.Vinculo = extract(`V[íi]nculo:\s*\n?\s*([A-ZÇÃÕÁÉÍÓÚÂÊÎÔÛ]+)`)

	// Nome
	atestado.Nome = extract(`Nome:\s*\n?\s*([A-ZÇÃÕÁÉÍÓÚÂÊÎÔÛ ]+)`)

	// Curso
	atestado.Curso = extract(`Curso:\s*\n?\s*([^\n]+)`)

	// Código de verificação
	atestado.CodigoVerificacao = extract(`c[oó]digo de verifica[cç][aã]o\s+(\w+)`)

	// Turmas
	// Cada turma tem o padrão:
	//   <código 5 dígitos>
	//   <NOME DA DISCIPLINA>
	//   <Nome do Professor>
	//   Tipo:
	//   <TIPO>
	//   (data)
	//   Local: <LOCAL>
	//   <turma número>
	//   <STATUS>
	//   <horário> (data)
	turmaRe := regexp.MustCompile(
		`(?m)^\s*(\d{5})\s*\n` + // código
			`\s*([A-ZÇÃÕÁÉÍÓÚÂÊÎÔÛÀÈÌÒÙÄËÏÖÜ /]+)\s*\n` + // nome disciplina
			`\s*([A-ZÇÃÕÁÉÍÓÚÂÊÎÔÛÀÈÌÒÙÄËÏÖÜ ]+)\s*\n` + // professor
			`[\s\S]*?Tipo:\s*\n\s*([^\n(]+)` + // tipo
			`[\s\S]*?Local:\s*([^\n]+)` + // local
			`[\s\S]*?\n\s*(\d+)\s*\n` + // número da turma
			`\s*(MATRICULADO|INDEFERIDO|CANCELADO|TRANCADO|DISPENSADO)\s*\n` + // status
			`\s*((?:[2-7][MmTtNn]\d+\s*)+)`, // horário
	)

	matches := turmaRe.FindAllStringSubmatch(htmlContent, -1)
	for _, m := range matches {
		turma := TurmaAtestado{
			Codigo:    strings.TrimSpace(m[1]),
			Nome:      strings.TrimSpace(m[2]),
			Professor: strings.TrimSpace(m[3]),
			Tipo:      strings.TrimSpace(m[4]),
			Local:     strings.TrimSpace(m[5]),
			Status:    strings.TrimSpace(m[7]),
			Horario:   strings.TrimSpace(m[8]),
		}
		atestado.Turmas = append(atestado.Turmas, turma)
	}

	return atestado, nil
}

func Login(username, password string) (string, error) {
	doc, jsessionid, err := doSigaaRequest("GET", URL_VIEW_LOGIN, "", "", nil, "")
	if err != nil {
		return "", fmt.Errorf("erro ao carregar página de login: %w", err)
	}

	actionUrlPath, exists := doc.Find("form[name='loginForm']").Attr("action")
	if !exists {
		return "", fmt.Errorf("não foi possível encontrar o formulário de login no HTML")
	}
	fullActionUrl := "https://sigs.ufrpe.br" + actionUrlPath

	payload := url.Values{}
	payload.Set("user.login", username)
	payload.Set("user.senha", password)
	payload.Set("width", "1920")
	payload.Set("height", "1080")
	payload.Set("urlRedirect", "")
	payload.Set("subsistemaRedirect", "")
	payload.Set("acao", "")
	payload.Set("acessibilidade", "")

	re := regexp.MustCompile(`;jsessionid=[^?]+`)
	cleanedActionUrl := re.ReplaceAllString(fullActionUrl, "")

	doc, newJsessionid, err := doSigaaRequest(
		"POST",
		cleanedActionUrl,
		jsessionid,
		URL_VIEW_LOGIN,
		strings.NewReader(payload.Encode()),
		"application/x-www-form-urlencoded",
	)
	if err != nil {
		return "", fmt.Errorf("erro ao submeter login: %w", err)
	}

	const selectorAviso = "input[type='submit'][value*='Continuar']"

	if doc.Find(selectorAviso).Length() > 0 {
		fmt.Println("⚠️ AvisoLogon detectado. Simulação do clique 'Continuar >>'.")

		// A URL final alcançada após o POST de login será o referer
		urlPtr := doc.Url
		var refererAviso string
		if urlPtr == nil {
			// Se a URL final é nula, usamos a URL da página de login como fallback seguro.
			// É menos preciso, mas evita o panic.
			refererAviso = URL_VIEW_LOGIN // Use a URL de login como fallback
		} else {
			refererAviso = urlPtr.String() // Agora a chamada .String() é segura
		}

		// Simular o clique para prosseguir
		_, newNewJsessionid, err := proceedFromAviso(
			doc,
			newJsessionid,
			refererAviso,
		)
		if err != nil {
			return "", err
		}

		// O resultado da navegação (dashboard ou falha) está em docProsseguiu
		newJsessionid = newNewJsessionid
	}

	return newJsessionid, nil
}

// proceedFromAviso simula o clique no botão "Continuar >>"
func proceedFromAviso(docAviso *goquery.Document, jsessionid, refererAviso string) (*goquery.Document, string, error) {

	// 1. Encontrar o formulário e a URL de action
	// O formulário tem o ID j_id_jsp_933481798_1
	form := docAviso.Find("form").First()

	actionPath, exists := form.Attr("action")
	if !exists {
		return nil, jsessionid, fmt.Errorf("não foi possível encontrar a action do formulário de aviso")
	}
	// A URL de action é relativa. Ex: /sigaa/telaAvisoLogon.jsf
	// Assumindo o domínio: https://sigs.ufrpe.br
	fullActionUrl := "https://sigs.ufrpe.br" + actionPath

	// 2. Extrair o nome dinâmico do botão "Continuar >>"
	botaoContinuar := form.Find("input[type='submit'][value*='Continuar']").First()

	nameBotao, exists := botaoContinuar.Attr("name")
	if !exists {
		// Isso deve acontecer se o seletor não funcionar ou se o HTML mudar.
		return nil, jsessionid, fmt.Errorf("erro: não foi possível encontrar o atributo 'name' do botão 'Continuar >>'")
	}

	// 3. Preparar o payload (dados a serem enviados no POST)
	payload := url.Values{}

	// a) O campo do próprio formulário (necessário para submissões JSF)
	// O formulário tem name="j_id_jsp_933481798_1" e um hidden input com o mesmo name
	payload.Set(form.AttrOr("name", ""), form.Find("input[type='hidden'][name='"+form.AttrOr("name", "")+"']").AttrOr("value", ""))

	// b) O campo dinâmico do botão clicado
	payload.Set(nameBotao, "Continuar >>")

	// c) O ViewState (CRUCIAL para JSF)
	viewStateValue := docAviso.Find("input[name='javax.faces.ViewState']").AttrOr("value", "")
	if viewStateValue == "" {
		return nil, jsessionid, fmt.Errorf("erro: não foi possível encontrar o campo ViewState")
	}
	payload.Set("javax.faces.ViewState", viewStateValue)

	// 4. Limpar action URL de JSESSIONID se necessário
	re := regexp.MustCompile(`;jsessionid=[^?]+`)
	cleanedActionUrl := re.ReplaceAllString(fullActionUrl, "")

	// 5. Executar o POST para prosseguir
	docFinal, newJsessionid, err := doSigaaRequest(
		"POST",
		cleanedActionUrl,
		jsessionid,
		refererAviso, // Referer deve ser a URL da página de aviso (telaAvisoLogon.jsf)
		strings.NewReader(payload.Encode()),
		"application/x-www-form-urlencoded",
	)
	if err != nil {
		return nil, newJsessionid, fmt.Errorf("erro ao simular clique em Continuar: %w", err)
	}

	return docFinal, newJsessionid, nil
}

func getPaginaPortal(jsessionid string) (*goquery.Document, string, string, error) {
	doc, newJsessionid, err := doSigaaRequest("GET", URL_PORTAL_DISCENTE, jsessionid, "", nil, "")
	if err != nil {
		return nil, jsessionid, "", err
	}
	viewState, err := parseViewState(doc, "discente")
	if err != nil {
		return nil, newJsessionid, "", err
	}
	return doc, newJsessionid, viewState, nil
}

func parseTurmas(doc *goquery.Document) ([]TurmaData, []Avaliacao, error) {
	turmasData := []TurmaData{}
	reFrontEnd := regexp.MustCompile(`'frontEndIdTurma':'([^']+)'`)
	reComponent := regexp.MustCompile(`'(form_acessarTurmaVirtual[^']*)':'([^']*)'`)
	var parseError error

	doc.Find("form[id^='form_acessarTurmaVirtual']").Each(func(i int, el *goquery.Selection) {
		linkElement := el.Find("a[onclick]")
		nomeTurma := strings.TrimSpace(linkElement.Text())
		formName, _ := el.Attr("name")
		onclickAttr, _ := linkElement.Attr("onclick")
		if nomeTurma == "" || formName == "" || onclickAttr == "" {
			return
		}

		frontEndMatches := reFrontEnd.FindStringSubmatch(onclickAttr)
		if len(frontEndMatches) < 2 {
			parseError = fmt.Errorf("erro ao parsear frontEndId da turma: %s", nomeTurma)
			return
		}
		frontEndId := frontEndMatches[1]

		componentMatchesList := reComponent.FindAllStringSubmatch(onclickAttr, -1)
		var componentId string
		for _, match := range componentMatchesList {
			if len(match) == 3 && match[1] == match[2] {
				componentId = match[1]
				break
			}
		}
		if componentId == "" {
			parseError = fmt.Errorf("erro ao parsear componentId da turma (par chave/valor não encontrado): %s", nomeTurma)
			return
		}

		tr := el.Closest("tr")
		local := strings.TrimSpace(tr.Find("td.info").First().Text())

		turmaInfo := TurmaInfo{
			Nome:        nomeTurma,
			FrontEndId:  frontEndId,
			FormName:    formName,
			ComponentId: componentId,
		}
		turmasData = append(turmasData, TurmaData{Nome: nomeTurma, Local: local, Faltas: FALTAS_INDEFINIDAS, Info: turmaInfo})
	})
	if parseError != nil {
		return nil, nil, parseError
	}

	doc.Find("td[class*='info'] center").Each(func(i int, horario *goquery.Selection) {
		partes := strings.FieldsSeq(horario.Text())
		for parte := range partes {
			if parte != "*" && parte != "" {
				turmasData[i].Horarios = append(turmasData[i].Horarios, parte)
			}
		}
	})

	var avaliacoes []Avaliacao

	doc.Find("#avaliacao-portal table tbody tr").Each(func(i int, s *goquery.Selection) {
		if i == 0 {
			return
		}
		var avaliacao Avaliacao
		cells := s.Find("td")
		textoData := strings.TrimSpace(cells.Eq(1).Text())
		partesData := strings.Fields(textoData)
		avaliacao.Data = strings.Join(partesData, " ")
		activityText := strings.TrimSpace(cells.Eq(2).Find("small").Text())
		partes := strings.SplitN(activityText, ":", 2)
		tipo := ""
		if len(partes) > 0 {
			campos := strings.Fields(partes[0])
			if len(campos) > 0 {
				tipo = campos[len(campos)-1]
			}
		}
		avaliacao.TurmaNome = strings.TrimSpace(strings.ReplaceAll(partes[0], tipo, ""))
		avaliacao.Tipo = strings.TrimSpace(tipo)
		if len(partes) > 1 {
			avaliacao.Nome = strings.TrimSpace(partes[1])
		}
		avaliacoes = append(avaliacoes, avaliacao)
	})

	return turmasData, avaliacoes, nil
}

func parseIndices(doc *goquery.Document) IndicesAcademicos {
	var indices IndicesAcademicos
	doc.Find("#agenda-docente > table > tbody > tr > td > table tr").Each(func(i int, s *goquery.Selection) {
		tds := s.Find("td")
		if tds.Length() == 4 {
			key1 := strings.TrimSpace(tds.Eq(0).Text())
			val1 := strings.TrimSpace(tds.Eq(1).Text())
			key2 := strings.TrimSpace(tds.Eq(2).Text())
			val2 := strings.TrimSpace(tds.Eq(3).Text())
			switch key1 {
			case "MC:":
				indices.MC = val1
			case "MCN:":
				indices.MCN = val1
			case "IEPL:":
				indices.IEPL = val1
			case "IEAN:":
				indices.IEAN = val1
			}
			switch key2 {
			case "IRA:":
				indices.IRA = val2
			case "IECH:":
				indices.IECH = val2
			case "IEA:":
				indices.IEA = val2
			case "IECHP:":
				indices.IECHP = val2
			}
		}
	})
	return indices
}

func parseCH(doc *goquery.Document) CargasHorarias {
	var ch CargasHorarias
	doc.Find("#agenda-docente > table > tbody > tr > td > table tr").Each(func(i int, s *goquery.Selection) {
		tds := s.Find("td")
		if tds.Length() == 2 {
			key := strings.TrimSpace(tds.Eq(0).Text())
			val := strings.TrimSpace(tds.Eq(1).Text())
			switch key {
			case "CH. Obrigatória Pendente":
				ch.ObrigatoriaPendente = val
			case "CH. Optativa Pendente":
				ch.OptativaPendente = val
			case "CH. Total Currículo":
				ch.TotalCurriculo = val
			case "CH. Complementar Pendente":
				ch.ComplementarPendente = val
			}
		}
	})
	return ch
}

func extractDynamicParams(doc *goquery.Document, payload *url.Values) error {
	var onclickContent string
	found := false

	doc.Find("table#table_lt tbody tr").EachWithBreak(func(i int, s *goquery.Selection) bool {
		if strings.Contains(s.Text(), "Ativa") {
			link := s.Find("a[title='Visualizar Estrutura Curricular']")
			if val, exists := link.Attr("onclick"); exists {
				onclickContent = val
				found = true
				return false
			}
		}
		return true
	})

	if !found {
		return fmt.Errorf("currículo ativo ou botão de visualização não encontrados")
	}

	reDict := regexp.MustCompile(`jsfcljs\(.*?,\s*\{([^}]+)\}`)
	matches := reDict.FindStringSubmatch(onclickContent)

	if len(matches) < 2 {
		return fmt.Errorf("não foi possível encontrar os parâmetros jsfcljs no onclick")
	}

	dictStr := matches[1] // String contendo: 'formCurriculos...':'form...', 'id':'28110657'

	reKV := regexp.MustCompile(`'([^']+)'\s*:\s*'([^']+)'`)
	kvMatches := reKV.FindAllStringSubmatch(dictStr, -1)

	if len(kvMatches) == 0 {
		return fmt.Errorf("nenhum par chave-valor encontrado dentro do jsfcljs")
	}

	for _, kv := range kvMatches {
		key := kv[1]
		value := kv[2]
		payload.Set(key, value)
	}

	return nil
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func parseCurriculoData(doc *goquery.Document) (EstruturaCurricular, error) {
	var curriculo EstruturaCurricular

	// 1. Extrair Dados Gerais da Tabela Principal
	doc.Find("table.formulario > tbody > tr").Each(func(i int, row *goquery.Selection) {
		th := cleanText(row.Find("th").First().Text())
		td := cleanText(row.Find("td").First().Text())

		switch th {
		case "Código:":
			curriculo.Codigo = td
		case "Matriz Curricular:":
			curriculo.MatrizCurricular = td
		case "Período Letivo de Entrada em Vigor:":
			curriculo.PeriodoVigor = td
		case "Total Mínima:":
			curriculo.CargaHorariaTotalMin = td
		case "Carga Horária Optativa Mínima:":
			curriculo.CargaHorariaOptativaMin = td
		case "Carga Horária Obrigatória Atividade Acadêmica Específica:":
			curriculo.CargaHorariaObrigatoria = td
		}
	})

	// Prazos estão aninhados em uma tabela interna, vamos buscar por regex ou seletores diretos
	doc.Find("table.formulario > tbody > tr table tbody tr").Each(func(i int, row *goquery.Selection) {
		row.Find("th").Each(func(j int, thSel *goquery.Selection) {
			thText := cleanText(thSel.Text())
			tdText := cleanText(thSel.NextFiltered("td").Text())

			if thText == "Mínimo:" {
				curriculo.PrazoMinimoSemestres = tdText
			} else if thText == "Médio:" {
				curriculo.PrazoMedioSemestres = tdText
			} else if thText == "Máximo:" {
				curriculo.PrazoMaximoSemestres = tdText
			}
		})
	})

	// 2. Extrair Disciplinas (Componentes Curriculares) das Abas
	doc.Find("div.yui-content > div").Each(func(i int, tabDiv *goquery.Selection) {
		tabID, exists := tabDiv.Attr("id") // Ex: "semestre1", "optativas"
		if !exists {
			return
		}

		// Identificar o nível/semestre
		nivel := strings.Replace(tabID, "semestre", "", 1)

		// Procurar por linhas de disciplinas (linhaPar ou linhaImpar)
		tabDiv.Find("tr.linhaPar, tr.linhaImpar").Each(func(j int, tr *goquery.Selection) {
			tds := tr.Find("td")
			if tds.Length() >= 2 {
				// td 0: "04341 - LÍNGUA BRASILEIRA DE SINAIS - LIBRAS - 60h"
				rawInfo := cleanText(tds.Eq(0).Text())
				// td 1: "Optativa" ou "Obrigatória" (dentro de um <i>)
				tipo := cleanText(tds.Eq(1).Text())

				// Quebrar a string "Código - Nome - Carga"
				parts := strings.Split(rawInfo, " - ")
				comp := ComponenteCurricular{
					Tipo:  tipo,
					Nivel: nivel,
				}

				if len(parts) >= 3 {
					comp.Codigo = strings.TrimSpace(parts[0])
					comp.CargaHoraria = strings.TrimSpace(parts[len(parts)-1])
					// O nome pode conter hífen, então juntamos o que sobrar no meio
					comp.Nome = strings.TrimSpace(strings.Join(parts[1:len(parts)-1], " - "))
				} else {
					// Fallback caso o padrão " - " fuja da regra
					comp.Nome = rawInfo
				}

				curriculo.Componentes = append(curriculo.Componentes, comp)
			}
		})
	})

	return curriculo, nil
}

func getPaginaCurriculo(jsessionid string) (*goquery.Document, string, string, error) {
	doc, newJsessionid, viewState, err := getPaginaPortal(jsessionid)
	if err != nil {
		fmt.Printf("Erro ao acessar página do portal: %v\n", err)
		return nil, newJsessionid, viewState, err
	}
	re := regexp.MustCompile(`portal\.jsf\?id=(\d+)`)
	match := re.FindStringSubmatch(doc.Text())

	if len(match) < 2 {
		return nil, newJsessionid, viewState, fmt.Errorf("id do curso não encontrado no HTML")
	}

	cursoID := match[1]
	cursoURL := fmt.Sprintf("https://sigs.ufrpe.br/sigaa/public/curso/curriculo.jsf?lc=pt_BR&id=%s", cursoID)
	doc, newJsessionid, err = doSigaaRequest("GET", cursoURL, newJsessionid, URL_PORTAL_DISCENTE, nil, "")
	if err != nil {
		fmt.Printf("Erro ao acessar página do currículo: %v\n", err)
		return nil, newJsessionid, viewState, err
	}
	viewState, err = parseViewState(doc, "curriculo")
	if err != nil {
		fmt.Printf("Erro ao parsear ViewState do currículo: %v\n", err)
		return nil, newJsessionid, viewState, err
	}
	payload := url.Values{}
	payload.Set("formCurriculosCurso", "formCurriculosCurso")
	payload.Set("nivel", "G")
	payload.Set("javax.faces.ViewState", viewState)
	err = extractDynamicParams(doc, &payload)
	if err != nil {
		fmt.Printf("Erro ao extrair parametros dinâmicos: %v\n", err)
		return nil, newJsessionid, viewState, err
	}
	doc, newJsessionid, err = doSigaaRequest(
		"POST",
		URL_CURRICULO,
		newJsessionid,
		URL_PORTAL_DISCENTE,
		strings.NewReader(payload.Encode()),
		"application/x-www-form-urlencoded",
	)
	if err != nil {
		fmt.Printf("Erro ao acessar página do currículo com POST: %v\n", err)
		return nil, newJsessionid, viewState, err
	}
	return doc, newJsessionid, viewState, nil
}

func getCurriculo(jsessionid string) (EstruturaCurricular, string, string, error) {
	var curriculo EstruturaCurricular
	doc, newJsessionid, viewState, err := getPaginaCurriculo(jsessionid)
	if err != nil {
		fmt.Printf("Erro ao obter página do currículo: %v\n", err)
		return curriculo, newJsessionid, viewState, err
	}
	curriculo, err = parseCurriculoData(doc)
	if err != nil {
		fmt.Printf("Erro ao parsear dados do currículo: %v\n", err)
		return curriculo, "", "", err
	}

	_, newJsessionid, viewState, err = getPaginaPortal(newJsessionid)
	if err != nil {
		fmt.Printf("Erro ao atualizar sessão após acessar currículo: %v\n", err)
		return curriculo, newJsessionid, viewState, err
	}

	return curriculo, newJsessionid, viewState, nil
}

func GetMainData(jsessionid string) (string, string, CargasHorarias, IndicesAcademicos, []Avaliacao, []TurmaData, string, string, error) {
	doc, newJsessionid, viewState, err := getPaginaPortal(jsessionid)
	var indices IndicesAcademicos
	var ch CargasHorarias
	if err != nil {
		return "", "", ch, indices, nil, nil, jsessionid, "", err
	}

	nomeEncontrado := strings.TrimSpace(doc.Find("p.usuario span").Text())
	if nomeEncontrado == "" {
		nomeEncontrado = strings.TrimSpace(doc.Find(".usuario > span").Text())
	}
	if nomeEncontrado == "" {
		return "", "", ch, indices, nil, nil, newJsessionid, viewState, fmt.Errorf("não foi possível encontrar o nome do aluno")
	}

	var matricula string

	doc.Find("#perfil-docente > #agenda-docente td").Each(func(i int, td *goquery.Selection) {
		texto := strings.TrimSpace(td.Text())

		if strings.Contains(texto, "Matrícula:") {
			next := td.Next()
			if next != nil {
				matricula = strings.TrimSpace(next.Text())
			}
		}
	})
	if matricula == "" {
		return "", matricula, ch, indices, nil, nil, newJsessionid, viewState, fmt.Errorf("não foi possível encontrar a matrícula do aluno")
	}

	turmasData, avaliacoes, err := parseTurmas(doc)
	if err != nil {
		return "", matricula, ch, indices, nil, nil, newJsessionid, viewState, fmt.Errorf("erro ao parsear turmas: %w", err)
	}

	indices = parseIndices(doc)

	ch = parseCH(doc)

	return nomeEncontrado, matricula, ch, indices, avaliacoes, turmasData, newJsessionid, viewState, nil
}

func parseNoticia(doc *goquery.Document) (Noticia, error) {
	var noticia Noticia
	noticiaDiv := doc.Find("#ultimaNoticia")
	if noticiaDiv.Length() == 0 {
		return noticia, nil
	}

	h4 := noticiaDiv.Find("h4")
	if h4.Length() > 0 {
		noticia.Titulo = strings.TrimSpace(h4.Contents().Last().Text())
	}

	noticiaDiv.Find(".conteudoNoticia p").Each(func(i int, p *goquery.Selection) {
		noticia.Conteudo = append(noticia.Conteudo, strings.TrimSpace(p.Text()))
	})

	return noticia, nil
}

func BaixarArquivoSigaa(jsessionid string, viewState string, chave string, fileId string, turma TurmaData) (*http.Response, string, string, error) {
	urlAva := URL_FREQUENCIA
	// 1. Pegamos o novo JSESSIONID e ViewState da turma
	_, _, newJsessionid, newViewState, err := getPaginaTurma(turma, jsessionid, viewState)
	if err != nil {
		return nil, jsessionid, viewState, fmt.Errorf("erro ao acessar página da turma para obter novo ViewState: %w", err)
	}

	payload := url.Values{}
	payload.Set("formAva", "formAva")
	payload.Set("formAva:idTopicoSelecionado", "0")
	payload.Set("javax.faces.ViewState", newViewState) // Usa o ViewState atualizado
	payload.Set(chave, chave)
	payload.Set("id", fileId)

	req, err := http.NewRequest("POST", urlAva, strings.NewReader(payload.Encode()))
	if err != nil {
		return nil, newJsessionid, newViewState, fmt.Errorf("erro ao criar requisição de download: %w", err)
	}

	// 2. Cabeçalhos idênticos aos usados na sua doSigaaRequest
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", USER_AGENT) // Usa a mesma constante do seu projeto!
	req.Header.Set("Referer", urlAva)

	// 3. Atualizamos o Cookie (passando a string diretamente, pois ela já contém "JSESSIONID=")
	if newJsessionid != "" {
		req.Header.Set("Cookie", newJsessionid)
	}

	client := &http.Client{
		// Impede o redirecionamento automático para pegarmos o erro 302 (expirada.jsp)
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, newJsessionid, newViewState, fmt.Errorf("erro ao executar requisição de download: %w", err)
	}

	// Se for status 302 (Found), o Sigaa está redirecionando
	if resp.StatusCode == http.StatusFound {
		redirectUrl, _ := resp.Location()
		resp.Body.Close()
		return nil, newJsessionid, newViewState, fmt.Errorf("sigaa negou o download e tentou redirecionar para: %s (Verifique os cookies)", redirectUrl)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, newJsessionid, newViewState, fmt.Errorf("falha no download, status code inesperado: %d", resp.StatusCode)
	}

	// Verifica se a resposta é realmente um arquivo ou se o Sigaa retornou um HTML de erro
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		resp.Body.Close()
		return nil, newJsessionid, newViewState, fmt.Errorf("o sigaa retornou uma página HTML em vez do arquivo")
	}

	_, finalJsessionid, finalViewState, err := getPaginaPortal(newJsessionid)

	if err != nil {
		return resp, newJsessionid, newViewState, nil
	}

	return resp, finalJsessionid, finalViewState, nil
}

func parseCronograma(doc *goquery.Document) ([]CronogramaItem, error) {
	var cronograma []CronogramaItem
	panel := doc.Find("#formAva\\:panelTopicosNaoSelecionados")
	if panel.Length() == 0 {
		return cronograma, nil
	}

	panel.Find("span").Each(func(i int, eventoSpan *goquery.Selection) {
		eventoDiv := eventoSpan.Children().First()
		if eventoDiv.Length() == 0 {
			return
		}

		// Busca por classe é muito mais seguro do que usar Eq(0) e Eq(1)
		titulo := strings.TrimSpace(eventoDiv.Find(".titulo").Text())
		conteudoDiv := eventoDiv.Find(".conteudotopico")

		if conteudoDiv.Length() == 0 {
			if titulo != "" {
				cronograma = append(cronograma, CronogramaItem{Titulo: titulo, Conteudo: ""})
			}
			return
		}

		// --- INÍCIO DA CORREÇÃO DE CONTEÚDO ---

		// 1. Clonamos a div para poder manipular e deletar lixos sem afetar o HTML original
		cleanDiv := conteudoDiv.Clone()

		// 2. Removemos elementos que têm texto/código que não queremos no nosso "Conteudo"
		// Isso inclui códigos JavaScript, elementos de arrastar e a div de arquivos em anexo
		// (pois você já extrai os arquivos separadamente abaixo)
		cleanDiv.Find("script, .drgind_fly, span[id*='listaMateriais']").Remove()

		// 3. Pegamos todo o texto restante da div limpa.
		// O uso de strings.Fields combinado com Join remove quebras de linha sujas (\n, \t)
		// e transforma tudo em uma string limpa separada por espaços simples.
		conteudo := strings.Join(strings.Fields(cleanDiv.Text()), " ")

		// --- FIM DA CORREÇÃO ---

		var arquivos []ArquivoCronograma

		// Busca todas as tags <a> que possuem um onclick contendo 'jsfcljs'
		eventoDiv.Find("a[onclick*='jsfcljs']").Each(func(k int, a *goquery.Selection) {
			nomeArquivo := strings.TrimSpace(a.Text())
			onclickJS, exists := a.Attr("onclick")

			if exists {
				chaveMatch := reJSFChave.FindStringSubmatch(onclickJS)
				idMatch := reJSFId.FindStringSubmatch(onclickJS)

				chave := ""
				id := ""

				if len(chaveMatch) > 1 {
					chave = chaveMatch[1]
				}
				if len(idMatch) > 1 {
					id = idMatch[1]
				}

				// Se conseguiu achar os identificadores, adiciona à lista
				if chave != "" && id != "" {
					arquivos = append(arquivos, ArquivoCronograma{
						Nome:  nomeArquivo,
						Chave: chave,
						ID:    id,
					})
				}
			}
		})

		if titulo != "" {
			cronograma = append(cronograma, CronogramaItem{Titulo: titulo, Conteudo: conteudo, Arquivos: arquivos})
		}
	})

	return cronograma, nil
}

func getPaginaTurma(turma TurmaData, jsessionid string, viewState string) (Noticia, []CronogramaItem, string, string, error) {
	payload := url.Values{}
	payload.Set(turma.Info.FormName, turma.Info.FormName)
	payload.Set(turma.Info.ComponentId, turma.Info.ComponentId)
	payload.Set("javax.faces.ViewState", viewState)
	payload.Set("frontEndIdTurma", turma.Info.FrontEndId)

	doc, newJsessionid, err := doSigaaRequest(
		"POST",
		URL_PORTAL_DISCENTE,
		jsessionid,
		URL_PORTAL_DISCENTE,
		strings.NewReader(payload.Encode()),
		"application/x-www-form-urlencoded",
	)
	if err != nil {
		var noticia Noticia
		var cronograma []CronogramaItem
		return noticia, cronograma, jsessionid, viewState, fmt.Errorf("erro ao acessar página da turma %s: %w", turma.Nome, err)
	}

	newViewState, err := parseViewState(doc, "turma_"+turma.Nome)
	noticia, _ := parseNoticia(doc)
	cronograma, _ := parseCronograma(doc)
	if err != nil {
		return noticia, cronograma, newJsessionid, "", err
	}

	return noticia, cronograma, newJsessionid, newViewState, nil
}

func getPaginaFrequencia(turma TurmaData, jsessionid string, viewState string) (int, string, string, error) {
	payload := url.Values{}
	payload.Set("formMenu", "formMenu")
	payload.Set("formMenu:j_id_jsp_1879301362_71", "formMenu:j_id_jsp_1879301362_94")
	payload.Set("javax.faces.ViewState", viewState)
	payload.Set("formMenu:j_id_jsp_1879301362_97", "formMenu:j_id_jsp_1879301362_97")

	doc, newJsessionid, err := doSigaaRequest(
		"POST",
		URL_FREQUENCIA,
		jsessionid,
		URL_PORTAL_DISCENTE,
		strings.NewReader(payload.Encode()),
		"application/x-www-form-urlencoded",
	)
	if err != nil {
		return 0, jsessionid, viewState, fmt.Errorf("erro ao acessar página de frequência %s: %w", turma.Nome, err)
	}
	html, _ := doc.Html()
	if strings.Contains(html, "A frequência ainda não foi lançada.") {
		newViewState, err := parseViewState(doc, "frequencia_"+turma.Nome)
		if err != nil {
			return PRESENCA_NAO_LANCADA, newJsessionid, "", err
		}
		return PRESENCA_NAO_LANCADA, newJsessionid, newViewState, nil
	}

	reFaltas := regexp.MustCompile(`(\d+)\s+Falta\(s\)`)
	matches := reFaltas.FindAllStringSubmatch(html, -1)
	totalFaltas := 0
	for _, m := range matches {
		if len(m) > 1 {
			faltas, err := strconv.Atoi(m[1])
			if err == nil {
				totalFaltas += faltas
			}
		}
	}

	newViewState, err := parseViewState(doc, "frequencia_"+turma.Nome)
	if err != nil {
		return totalFaltas, newJsessionid, "", err
	}

	return totalFaltas, newJsessionid, newViewState, nil
}

func getPaginaNotas(jsessionid string, viewState string) (*goquery.Document, string, error) {
	payload := url.Values{}
	payload.Set("menu:form_menu_discente", "menu:form_menu_discente")
	payload.Set("id", "107543")
	payload.Set("jscook_action", "menu_form_menu_discente_discente_menu:A]#{ relatorioNotasAluno.gerarRelatorio }")
	payload.Set("javax.faces.ViewState", viewState)

	doc, newJsessionid, err := doSigaaRequest(
		"POST",
		URL_PORTAL_DISCENTE,
		jsessionid,
		URL_PORTAL_DISCENTE,
		strings.NewReader(payload.Encode()),
		"application/x-www-form-urlencoded",
	)
	if err != nil {
		return nil, jsessionid, fmt.Errorf("erro ao acessar página de notas: %w", err)
	}
	return doc, newJsessionid, nil
}

func GetNotas(jsessionid string, viewState string) ([]DisciplinaNotas, []DisciplinaNotas, string, string, error) {
	doc, newJsessionid, err := getPaginaNotas(jsessionid, viewState)
	if err != nil {
		return nil, nil, jsessionid, viewState, err
	}

	disciplinas := []DisciplinaNotas{}
	anteriores := []DisciplinaNotas{}
	headerNames := []string{}
	doc.Find("table.tabelaRelatorio").Each(func(i int, s *goquery.Selection) {
		s.Find("thead tr th").Each(func(j int, s *goquery.Selection) {
			headerNames = append(headerNames, strings.TrimSpace(s.Text()))
		})
		s.Find("tbody tr.linha").Each(func(j int, row *goquery.Selection) {
			disciplina := DisciplinaNotas{
				Notas: make(map[string]string),
			}
			row.Find("td").Each(func(k int, cell *goquery.Selection) {
				if k >= len(headerNames) {
					return
				}
				headerName := headerNames[k]
				cellValue := strings.TrimSpace(cell.Text())
				switch headerName {
				case "Código":
					disciplina.Codigo = cellValue
				case "Disciplina":
					disciplina.Nome = cellValue
				case "Resultado":
					disciplina.Resultado = cellValue
				case "Faltas":
					disciplina.Faltas = cellValue
				case "Situação":
					disciplina.Situacao = cellValue
				default:
					if cellValue != "" && cellValue != "--" {
						disciplina.Notas[headerName] = cellValue
					}
				}
			})

			if disciplina.Nome != "" {
				if i == 0 {
					disciplinas = append(disciplinas, disciplina)
				} else {
					anteriores = append(anteriores, disciplina)
				}
			}
		})
	})

	return disciplinas, anteriores, newJsessionid, viewState, nil
}

func GetTurmaData(turma TurmaData, jsessionid string, viewState string) (TurmaData, string, string, error) {
	noticia, cronograma, jsessionid1, viewState1, err := getPaginaTurma(turma, jsessionid, viewState)
	turma.Cronograma = cronograma
	turma.Noticia = noticia
	if err != nil {
		return turma, jsessionid, viewState, err
	}

	faltas, jsessionid2, viewState2, err := getPaginaFrequencia(turma, jsessionid1, viewState1)
	if err != nil {
		return turma, jsessionid1, viewState1, err
	}
	turma.Faltas = faltas

	_, jsessionid3, viewState3, err := getPaginaPortal(jsessionid2)
	if err != nil {
		return turma, jsessionid2, viewState2, fmt.Errorf("erro ao voltar para o portal principal: %w", err)
	}

	return turma, jsessionid3, viewState3, nil
}
