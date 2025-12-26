package main 

import (
	"sync"
	"fmt"
	"net/http"
)

type RequestCache struct {
	data map[string]map[string][]byte
	// A estrutura é um mapa onde a chave externa é o identificador do usuário (por exemplo, jsessionid)
	// e a chave interna é o endpoint da requisição. O valor é o conteúdo em bytes da resposta.
	// Exemplo: data["jsessionid123"]["/minhas-turmas"] = []byte("conteúdo da resposta")
}

func NewRequestCache() *RequestCache { // Construtor para inicializar o cache, ele retorna o ponteiro para um struct RequestCache
	return &RequestCache{
		data: make(map[string]map[string][]byte),
	}
}

func (rc *RequestCache) CreateStudentCache(userID string){
	c.data[userID] = make(map[string][]byte)
}

func (rc *RequestCache) Set(userID string, endpoint string, content []byte) {
	rc.data[userID][endpoint] = content
}

func (rc *RequestCache) Get(userID string, endpoint string) ([]byte, bool) {
	content, exists := rc.data[userID][endpoint]
	return content, exists
}
