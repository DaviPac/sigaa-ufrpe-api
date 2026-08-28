# SIGAA UFRPE — API do Backend

Guia de consumo para o frontend. Descreve autenticação, o modelo de estado do
SIGAA, todos os endpoints, formatos de request/response e códigos de erro.

> Swagger interativo: `GET /swagger/index.html`

---

## 1. Visão geral

O backend é um proxy/scraper sobre o SIGAA. Ele **não tem banco de usuários**: o
login devolve um identificador de sessão do próprio SIGAA (`jsessionid`) que o
frontend guarda e reenvia a cada chamada.

- **Base URL (produção):** `https://sigaa-ufrpe-api-production.up.railway.app`
- **Base URL (local):** `http://localhost:8080` (porta configurável via `PORT`)
- **Formato:** JSON em todas as rotas, exceto downloads (PDF/arquivos) e o SSE.
- **CORS:** liberado apenas para as origens abaixo (com credenciais).
  As rotas de calendário são liberadas para qualquer origem.

```
https://sigaa-lite-ufrpe.vercel.app
https://conecta-ufrpe.vercel.app
https://mozilla.github.io
http://localhost:4200
```

O subsistema `/classroom/*` (Google Classroom) é **independente**: usa OAuth do
Google e depende de banco de dados. Se o banco estiver fora, só essas rotas
degradam (HTTP 503); todo o resto continua funcionando.

---

## 2. Autenticação

### 2.1. Login

```http
POST /login
Content-Type: application/json

{ "username": "seu.usuario", "password": "sua.senha" }
```

**200 OK**
```json
{ "jsessionid": "JSESSIONID=B00D525B6B0B9EBFBDED9DC3DC9B29BF.producao-jboss01" }
```

Trate o valor de `jsessionid` como **opaco**. Guarde-o e envie em todas as rotas
autenticadas no header:

```http
Authorization: Bearer JSESSIONID=B00D525B6B0B9EBFBDED9DC3DC9B29BF.producao-jboss01
```

> O header aceita tanto `Bearer JSESSIONID=<valor>` quanto só `Bearer <valor>`.
> Prefira mandar a string completa que veio no login.

**Erros do login**

| Código | Corpo | Significado |
|--------|-------|-------------|
| 400 | `{"error":"username e password são obrigatórios"}` | Campos faltando |
| 400 | `{"error":"JSON inválido"}` | Body malformado |
| 401 | `{"error":"Usuário ou senha inválidos"}` | Credenciais erradas |
| 502 | `{"error":"Falha ao se comunicar com o SIGAA. Tente novamente mais tarde."}` | SIGAA fora do ar |

---

## 3. ⚠️ Modelo de estado (LEIA ISTO)

O SIGAA é **stateful** e baseado em JSF. Duas coisas mudam a cada requisição
autenticada e **precisam ser repassadas para a chamada seguinte**:

| Dado | Onde vem | Onde vai na próxima chamada |
|------|----------|------------------------------|
| `jsessionid` | corpo da resposta de cada rota | header `Authorization: Bearer ...` |
| `viewState` | corpo da resposta de cada rota | corpo (`body`) da próxima rota POST |

**Regra de ouro:** depois de **toda** resposta que contenha `jsessionid` e
`viewState`, sobrescreva os valores guardados no frontend. Se você reutilizar um
`viewState` antigo, o SIGAA pode ignorar a navegação e você recebe erro (ou, no
pior caso, dados inconsistentes — o backend detecta e devolve erro em vez de
dado errado, veja §5.3).

Fluxo típico:

```
POST /login                      -> jsessionid
GET  /main-data (Bearer)         -> jsessionid', viewState'   (turmas, índices…)
POST /notas    { viewState' }    -> jsessionid'', viewState''
POST /turma    { viewState'' }   -> jsessionid''', viewState'''
...
```

As rotas que devolvem **PDF puro** (`/historico`, `/vinculo`, `/calendario`)
**não** devolvem novo estado — chame-as por último ou refaça um `GET /main-data`
depois para obter um `viewState` fresco.

---

## 4. Endpoints SIGAA (autenticados)

Todos exigem `Authorization: Bearer <jsessionid>`.

### 4.1. `GET /main-data` — dados do portal

Ponto de entrada depois do login. Traz nome, matrícula, turmas do semestre,
índices acadêmicos e o `viewState` inicial.

**200 OK**
```json
{
  "nome": "DAVI PIRES A. DE CARVALHO",
  "matricula": "20230008900",
  "turmas": [ /* TurmaData[] — ver §6.1 */ ],
  "avaliacoes": [ /* Avaliacao[] — ver §6.4 */ ] ,
  "indices": {
    "mc": "7.8526", "ira": "7.4459", "mcn": "372.9063", "iech": "0.9344",
    "iepl": "0.7991", "iea": "5.8634", "iean": "278.4413", "iechp": "1.0"
  },
  "cargaHoraria": {
    "optativaPendente": "840", "obrigatoriaPendente": "480",
    "complementarPendente": "180", "totalCurriculo": "3210"
  },
  "jsessionid": "JSESSIONID=...",
  "viewState": "j_id2"
}
```

`avaliacoes` pode vir `null` quando não há provas/trabalhos anunciados.
Nas turmas do `main-data`, `cronograma` e `noticia` vêm **vazios** — use
`POST /turma` ou `GET /turmas-stream` para preenchê-los.

| Código | Corpo | Significado |
|--------|-------|-------------|
| 401 | `{"error":"Sessão expirada ou inválida"}` | `jsessionid` inválido/expirado — refaça o login |

---

### 4.2. `POST /turma` — detalha **uma** turma

Enrique uma turma com **cronograma**, **notícia** e **faltas**.

```http
POST /turma
Authorization: Bearer <jsessionid>
Content-Type: application/json

{
  "turma": { /* um objeto TurmaData vindo de /main-data, sem alterações */ },
  "viewState": "j_id2"
}
```

**200 OK**
```json
{
  "turma": { /* TurmaData completo — mesmo objeto, agora com cronograma/noticia/faltas */ },
  "jsessionid": "JSESSIONID=...",
  "viewState": "j_id7"
}
```

O campo `turma.nome` da resposta é **garantidamente** o mesmo da turma pedida
(o backend valida a identidade da página carregada; veja §5.3).

| Código | Corpo |
|--------|-------|
| 400 | `{"error":"JSON inválido: ..."}` |
| 500 | `{"error":"Erro ao buscar dados da turma: ..."}` |

---

### 4.3. `GET /turmas-stream` — todas as turmas via SSE

Faz o que o `POST /turma` faz, para **todas** as turmas, em streaming
(`text/event-stream`). Ideal para a tela inicial: renderize cada turma conforme
chega.

```http
GET /turmas-stream
Authorization: Bearer <jsessionid>
```

Eventos emitidos:

| `event:` | `data:` | Quando |
|----------|---------|--------|
| `start` | `{"total": 6}` | 1x no início — quantas turmas virão |
| `turma` | `TurmaData` completo (§6.1) | 1x por turma processada com sucesso |
| `error` | `{"turma":"NOME","error":"..."}` | Falha em **uma** turma; o stream **continua** nas demais |
| `error` | `{"error":"..."}` | Falha geral (ex.: não deu para re-sincronizar) — stream encerra |
| `done`  | `{"message":"...","jsessionid":"...","viewState":"..."}` | 1x no fim |

**Guarde `jsessionid` e `viewState` do evento `done`** para as próximas chamadas.
Se o cliente fechar a conexão, o backend aborta a raspagem.

Exemplo (browser):
```js
const es = new EventSource(`${BASE}/turmas-stream`, { withCredentials: true });
// EventSource não manda header Authorization — use fetch + ReadableStream,
// ou um polyfill que aceite headers. Veja a nota abaixo.
```

> **Nota:** `EventSource` nativo não envia `Authorization`. Use `fetch()` com
> `headers: { Authorization: 'Bearer ' + jsessionid }` e leia
> `response.body.getReader()`, ou uma lib de SSE que aceite headers
> (ex.: `@microsoft/fetch-event-source`).

---

### 4.4. `POST /notas` — relatório de notas

```http
POST /notas
Authorization: Bearer <jsessionid>
Content-Type: application/json

{ "viewState": "j_id2" }
```

**200 OK**
```json
{
  "message": "HTML de notas baixado com sucesso!",
  "jsessionid": "JSESSIONID=...",
  "viewState": "j_id9",
  "notas":      [ /* DisciplinaNotas[] — semestre atual */ ],
  "anteriores": [ /* DisciplinaNotas[] — semestres anteriores */ ]
}
```

`DisciplinaNotas` (§6.2). O mapa `notas` tem **chaves dinâmicas** (nomes das
unidades/avaliações como o SIGAA exibe: `"Unid. 1"`, `"Reav."`, etc.).

| Código | Corpo |
|--------|-------|
| 400 | `{"error":"JSON inválido"}` (viewState ausente) |
| 401 | `{"error":"Sessão expirada ou inválida"}` |

---

### 4.5. `POST /matricula` — atestado de matrícula (estruturado)

```http
POST /matricula
Authorization: Bearer <jsessionid>
Content-Type: application/json

{ "viewState": "j_id2" }
```

**200 OK** — objeto `AtestadoMatricula`:
```json
{
  "periodoLetivo": "2025.1",
  "matricula": "20230008900",
  "vinculo": "REGULAR",
  "nome": "DAVI PIRES A. DE CARVALHO",
  "nivel": "GRADUAÇÃO",
  "curso": "CIÊNCIA DA COMPUTAÇÃO/...",
  "turmas": [
    {
      "codigo": "12345", "nome": "TESTE DE SOFTWARE",
      "professor": "FULANO DE TAL", "local": "LAB 41- CEAGRI 2",
      "tipo": "...", "status": "MATRICULADO", "horario": "2T45 5T23"
    }
  ],
  "codigoVerificacao": "abc123..."
}
```

Esta rota **não** devolve `jsessionid`/`viewState` novos.

| Código | Corpo |
|--------|-------|
| 400 | `{"error":"JSON inválido: ..."}` |
| 500 | `{"error":"Erro ao buscar matrícula: ..."}` / `{"error":"Erro ao parsear atestado: ..."}` |

---

### 4.6. `GET /curriculo` — estrutura curricular do curso

```http
GET /curriculo
Authorization: Bearer <jsessionid>
```

**200 OK**
```json
{
  "estruturaCurricular": {
    "codigo": "...", "matrizCurricular": "...", "periodoVigor": "2017.1",
    "cargaHorariaTotalMin": "3210", "cargaHorariaOptativaMin": "...",
    "cargaHorariaObrigatoria": "...", "prazoMinimoSemestres": "8",
    "prazoMedioSemestres": "10", "prazoMaximoSemestres": "14",
    "componentes": [
      {
        "codigo": "04341", "id": "28110657",
        "nome": "LÍNGUA BRASILEIRA DE SINAIS - LIBRAS",
        "cargaHoraria": "60h", "tipo": "Optativa", "nivel": "optativas"
      }
    ]
  },
  "jsessionid": "JSESSIONID=...",
  "viewState": "j_idX"
}
```

`componentes[].nivel` é o semestre (`"1"`, `"2"`, …) ou `"optativas"` /
`"complementares"`. Use `componentes[].id` + `estruturaCurricular.codigo` para
chamar `/componente`.

| Código | Corpo |
|--------|-------|
| 500 | `{"error":"Erro ao acessar currículo: ..."}` |

---

### 4.7. `POST /componente` — detalhes de um componente curricular

```http
POST /componente
Authorization: Bearer <jsessionid>
Content-Type: application/json

{
  "idComponente": "28110657",   // = componentes[].id do /curriculo
  "curriculo":    "...",         // = estruturaCurricular.codigo do /curriculo
  "viewState":    "j_idX"
}
```

**200 OK**
```json
{
  "componente": {
    "codigo": "04341", "nome": "LÍNGUA BRASILEIRA DE SINAIS - LIBRAS",
    "tipo": "MÓDULO", "modalidade": "PRESENCIAL",
    "unidade": "DEPARTAMENTO DE ...", "ementa": "…",
    "cargaHorariaTotal": "60h",
    "preRequisitos": ["EXAT0001", "..."],
    "equivalencias": ["..."]
  },
  "jsessionid": "JSESSIONID=...",
  "viewState": "j_idY"
}
```

`preRequisitos` e `equivalencias` são filtrados pelo `curriculo` informado e
**nunca** vêm `null` (no mínimo `[]`).

| Código | Corpo |
|--------|-------|
| 400 | `{"error":"JSON inválido: ..."}` |
| 500 | `{"error":"Erro ao acessar detalhes do componente: ..."}` |

---

### 4.8. `POST /historico` e `POST /vinculo` — PDFs

```http
POST /historico          (ou /vinculo)
Authorization: Bearer <jsessionid>
Content-Type: application/json

{ "viewState": "j_id2" }
```

**200 OK** — corpo **binário**:
```
Content-Type: application/pdf
Content-Disposition: attachment; filename=historico.pdf
```

Não devolvem estado novo. Consuma como `blob`:
```js
const r = await fetch(`${BASE}/historico`, {
  method: 'POST',
  headers: { Authorization: `Bearer ${jsessionid}`, 'Content-Type': 'application/json' },
  body: JSON.stringify({ viewState }),
});
const url = URL.createObjectURL(await r.blob());
```

| Código | Corpo |
|--------|-------|
| 400 | `{"error":"JSON inválido: ..."}` |
| 500 | `{"error":"Erro ao buscar histórico: ..."}` (ex.: `viewState` velho → SIGAA devolveu HTML em vez de PDF) |

---

### 4.9. Download de arquivo do cronograma (2 passos)

Arquivos aparecem em `turma.cronograma[].arquivos[]` (§6.3). O download é feito
em duas etapas para funcionar em WebView/mobile.

**Passo 1 — preparar** (autenticado):
```http
POST /turma/arquivo/preparar
Authorization: Bearer <jsessionid>
Content-Type: application/json

{
  "viewState": "j_id7",
  "chave": "formAva:j_id_jsp_1879301362_314:2:...",   // arquivos[].chave
  "id":    "78BAF65DEA73ACA64CCC34BC50B6916D82289CDC", // arquivos[].id
  "turma": { /* TurmaData da turma dona do arquivo */ }
}
```

**200 OK**
```json
{
  "ticket": "550e8400-e29b-41d4-a716-446655440000",
  "newJsessionid": "JSESSIONID=...",
  "newViewState": "j_id8"
}
```

**Passo 2 — baixar** (público, sem header; abra numa aba/`window.location`):
```http
GET /turma/arquivo/download?ticket=550e8400-e29b-41d4-a716-446655440000
```

Retorna o arquivo binário com o `filename` original do SIGAA. O `ticket` é
**uso único** e **expira em 2 minutos**.

| Código | Corpo |
|--------|-------|
| 400 | `{"error":"JSON inválido: ..."}` |
| 502 | `{"error":"Erro ao baixar arquivo do SIGAA: ..."}` |
| 404 (no passo 2) | `Link expirado ou inválido` (texto puro) |

---

## 5. Endpoints públicos (sem autenticação)

### 5.1. `GET /ping`
`200` → `{ "message": "pong" }`. Health check.

### 5.2. Calendário acadêmico

| Rota | Resposta |
|------|----------|
| `GET /calendario/url` | `200 {"url":"https://preg.ufrpe.br/.../calendario.pdf"}` |
| `GET /calendario` | `200` PDF (`Content-Type: application/pdf`, inline) |

Ambas liberadas para qualquer origem (CORS `*`). `502` se a PREG estiver fora.

### 5.3. Sobre a proteção "turma certa"

Ao entrar numa turma virtual, o backend confere que a página carregada é
**mesmo** da turma pedida (compara o nome). Se o SIGAA devolver a turma errada
(acontece quando o `viewState` está dessincronizado), a resposta é um **erro
500**, nunca dado de outra turma. Se acontecer, refaça `GET /main-data` para
pegar um `viewState` novo e tente de novo.

---

## 6. Modelos de dados

### 6.1. `TurmaData`
```jsonc
{
  "nome": "COMPUTAÇÃO PARA ANÁLISE DE DADOS",
  "local": "LAB 35- CEAGRI 2",
  "horarios": ["2N34", "5N12"],        // códigos SIGAA — ver §6.5
  "notas": { /* DisciplinaNotas — vazio no /main-data */ },
  "faltas": -2,                         // ver tabela abaixo
  "info": {                             // identificadores internos p/ navegação
    "nome": "COMPUTAÇÃO PARA ANÁLISE DE DADOS",
    "frontEndId": "87059F2E...",
    "formName": "form_acessarTurmaVirtual",
    "componentId": "form_acessarTurmaVirtual:j_id_jsp_340461267_483"
  },
  "noticia": { "titulo": "", "conteudo": null },  // preenchido por /turma
  "cronograma": null                               // preenchido por /turma
}
```

> **Envie o objeto `TurmaData` de volta sem modificar** ao chamar `/turma` e
> `/turma/arquivo/preparar` — o campo `info` é o que o backend usa para navegar.

Valores especiais de `faltas`:

| Valor | Significado |
|-------|-------------|
| `-2` | Ainda não consultado (estado inicial no `/main-data`) |
| `-1` | Frequência ainda não lançada pelo professor |
| `>= 0` | Total de faltas |

### 6.2. `DisciplinaNotas`
```jsonc
{
  "codigo": "12345",
  "nome": "TESTE DE SOFTWARE",
  "notas": { "Unid. 1": "8.0", "Unid. 2": "7.5" },  // chaves dinâmicas
  "resultado": "7.8",
  "faltas": "4",
  "situacao": "APROVADO"
}
```

### 6.3. `CronogramaItem` e `ArquivoCronograma`
```jsonc
{
  "titulo": "Aula 1 - Introdução",
  "conteudo": "texto do tópico já limpo",
  "arquivos": [
    { "nome": "slides.pdf", "chave": "formAva:...:2:...", "id": "78BAF6..." }
  ]
}
```

### 6.4. `Avaliacao`
```jsonc
{
  "nome": "Prova 1",
  "turmaNome": "TESTE DE SOFTWARE",
  "data": "20/03/2025 10:00",
  "tipo": "AVALIACAO"
}
```

### 6.5. Códigos de horário SIGAA

Formato `<dias><turno><períodos>`, ex.: `2N34`, `7M2345`.

- **Dia:** `2`=Seg, `3`=Ter, `4`=Qua, `5`=Qui, `6`=Sex, `7`=Sáb (`1`=Dom)
- **Turno:** `M`=manhã, `T`=tarde, `N`=noite
- **Períodos:** dígitos sequenciais das aulas naquele turno

`2N34` = segunda à noite, 3º e 4º horários. `2T45 5T23` = duas ocorrências.

---

## 7. Google Classroom (`/classroom/*`)

Subsistema separado. Identifica o aluno pela **`matricula`** no corpo, **não**
pelo `jsessionid`. Requer que o backend tenha banco + credenciais OAuth do
Google configurados.

### 7.1. Fluxo de vínculo (uma vez por aluno)

```
1. GET  /classroom/auth-url?matricula=20230008900
   -> { "auth_url": "https://accounts.google.com/o/oauth2/..." }

2. Frontend redireciona o usuário para auth_url.

3. Usuário consente. Google chama GET /classroom/callback?state=<matricula>&code=...
   O backend salva o token e faz 302 para:
   https://sigaa-lite-ufrpe.vercel.app/turma?google_sync=success
```

Depois disso, as rotas de dados abaixo funcionam para aquela matrícula.

### 7.2. Rotas de dados

Todas são `POST`, corpo `{ "matricula": "...", "course_id": "..." }`
(`course_id` dispensável só em `/courses`). `course_id` é o `id` de `/courses`.

| Rota | Body extra | Resposta (array) |
|------|-----------|------------------|
| `POST /classroom/courses` | — | `[{ "id", "name", "room" }]` |
| `POST /classroom/assignments` | `course_id` | `[{ "title", "description", "due_date", "alternateLink" }]` |
| `POST /classroom/announcements` | `course_id` | `[{ "id", "text", "creationTime", "alternateLink" }]` |
| `POST /classroom/topics` | `course_id` | `[{ "topicId", "name" }]` |
| `POST /classroom/materials` | `course_id` | `[{ "id", "title", "description", "alternateLink", "creationTime" }]` |
| `POST /classroom/submissions` | `course_id` | `[{ "id", "courseWorkId", "state", "grade" }]` |

`due_date` vem como `"DD/MM/AAAA"` (string vazia se sem prazo).
`state` de submissão: `NEW`, `CREATED`, `TURNED_IN`, `RETURNED`, `RECLAIMED_BY_STUDENT`.
Arrays nunca vêm `null` — no mínimo `[]`.

### 7.3. Erros do Classroom

| Código | Corpo | Significado |
|--------|-------|-------------|
| 400 | `{"error":"Corpo inválido. Esperado: matricula e course_id"}` | Body faltando campo |
| 400 | `{"error":"course_id é obrigatório"}` | `course_id` vazio |
| 401 | `{"error":"Aluno não autenticado no Google", "details":"..."}` | Sem token salvo — rode o fluxo §7.1 |
| 502 | `{"error":"Erro ao buscar ... na API do Google"}` | Falha na API do Google |
| 503 | `{"error":"Serviço de integração com o Google temporariamente indisponível"}` | Banco fora do ar |

---

## 8. Tabela de códigos HTTP (resumo)

| Código | Onde | Ação recomendada no frontend |
|--------|------|------------------------------|
| 200 | — | Sucesso |
| 400 | qualquer POST | Corrigir o body (JSON / campos obrigatórios) |
| 401 | rotas SIGAA | `jsessionid` inválido/expirado → refazer `POST /login` |
| 401 | rotas Classroom | Rodar o fluxo OAuth (§7.1) |
| 404 | download de arquivo | Ticket expirado (>2 min) → refazer `/preparar` |
| 500 | rotas SIGAA | Erro de scraping — refazer `GET /main-data` p/ estado novo e repetir |
| 502 | login / calendário / classroom | SIGAA/PREG/Google fora — retry com backoff |
| 503 | rotas Classroom | Banco indisponível — tentar mais tarde |

---

## 9. Checklist de integração

- [ ] Guardar `jsessionid` do `/login` e mandar em `Authorization: Bearer` em toda rota SIGAA.
- [ ] Após cada resposta, **sobrescrever** `jsessionid` e `viewState` guardados.
- [ ] Mandar `viewState` no **body** dos POSTs; `jsessionid` no **header**.
- [ ] Reenviar objetos `TurmaData` **sem alterar** (o campo `info` é essencial).
- [ ] Em 401 nas rotas SIGAA → recomeçar do `/login`.
- [ ] Em 500 nas rotas SIGAA → `GET /main-data` para renovar o `viewState` e repetir.
- [ ] `/historico`, `/vinculo`, `/calendario` retornam **blob**, não JSON, e não renovam estado.
- [ ] SSE `/turmas-stream`: usar `fetch` com header (não `EventSource` puro) e salvar o estado do evento `done`.
- [ ] Classroom é à parte: identifica por `matricula`, exige fluxo OAuth prévio.
