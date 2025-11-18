package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

const (
	URL_VIEW_LOGIN      = "https://sigs.ufrpe.br/sigaa/verTelaLogin.do"
	URL_PORTAL_DISCENTE = "https://sigs.ufrpe.br/sigaa/portais/discente/discente.jsf"
	URL_FREQUENCIA      = "https://sigs.ufrpe.br/sigaa/ava/index.jsf"
	USER_AGENT          = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
)

func doSigaaRequest(method, url, jsessionid, referer string, body io.Reader, contentType string) (*goquery.Document, string, error) {
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

		turmaInfo := TurmaInfo{
			Nome:        nomeTurma,
			FrontEndId:  frontEndId,
			FormName:    formName,
			ComponentId: componentId,
		}
		turmasData = append(turmasData, TurmaData{Nome: nomeTurma, Faltas: FALTAS_INDEFINIDAS, Info: turmaInfo})
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

func GetMainData(jsessionid string) (string, CargasHorarias, IndicesAcademicos, []Avaliacao, []TurmaData, string, string, error) {
	doc, newJsessionid, viewState, err := getPaginaPortal(jsessionid)
	var indices IndicesAcademicos
	var ch CargasHorarias
	if err != nil {
		return "", ch, indices, nil, nil, jsessionid, "", err
	}

	nomeEncontrado := strings.TrimSpace(doc.Find("p.usuario span").Text())
	if nomeEncontrado == "" {
		nomeEncontrado = strings.TrimSpace(doc.Find(".usuario > span").Text())
	}
	if nomeEncontrado == "" {
		return "", ch, indices, nil, nil, newJsessionid, viewState, fmt.Errorf("não foi possível encontrar o nome do aluno")
	}

	turmasData, avaliacoes, err := parseTurmas(doc)
	if err != nil {
		return "", ch, indices, nil, nil, newJsessionid, viewState, fmt.Errorf("erro ao parsear turmas: %w", err)
	}

	indices = parseIndices(doc)

	ch = parseCH(doc)

	return nomeEncontrado, ch, indices, avaliacoes, turmasData, newJsessionid, viewState, nil
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
		titulo := strings.TrimSpace(eventoDiv.Children().Eq(0).Text())
		conteudoDiv := eventoDiv.Children().Eq(1)
		if conteudoDiv.Length() == 0 {
			if titulo != "" {
				cronograma = append(cronograma, CronogramaItem{Titulo: titulo, Conteudo: ""})
			}
			return
		}

		var conteudo string
		p := conteudoDiv.Find("p")
		if p.Length() > 0 {
			conteudo = strings.TrimSpace(p.First().Text())
		} else {
			var textParts []string
			conteudoDiv.Contents().Each(func(j int, s *goquery.Selection) {
				if s.Get(0) != nil && s.Get(0).Type == html.TextNode {
					text := strings.TrimSpace(s.Text())
					if text != "" {
						textParts = append(textParts, text)
					}
				}
			})
			conteudo = strings.Join(textParts, " ")
		}

		if titulo != "" {
			cronograma = append(cronograma, CronogramaItem{Titulo: titulo, Conteudo: conteudo})
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

func GetNotas(jsessionid string, viewState string) ([]DisciplinaNotas, string, string, error) {
	doc, newJsessionid, err := getPaginaNotas(jsessionid, viewState)
	if err != nil {
		return nil, jsessionid, viewState, err
	}

	disciplinas := []DisciplinaNotas{}
	headerNames := []string{}

	table := doc.Find("table.tabelaRelatorio").First()
	table.Find("thead tr th").Each(func(i int, s *goquery.Selection) {
		headerNames = append(headerNames, strings.TrimSpace(s.Text()))
	})
	table.Find("tbody tr.linha").Each(func(i int, row *goquery.Selection) {
		disciplina := DisciplinaNotas{
			Notas: make(map[string]string),
		}

		row.Find("td").Each(func(j int, cell *goquery.Selection) {
			if j >= len(headerNames) {
				return
			}

			headerName := headerNames[j]
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
			disciplinas = append(disciplinas, disciplina)
		}
	})

	return disciplinas, newJsessionid, viewState, nil
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
