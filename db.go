package main

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/oauth2"
)

// DB é a nossa conexão global com o banco de dados
var DB *sql.DB

// InitDB conecta ao PostgreSQL usando a variável DATABASE_URL da Railway
func InitDB() error {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		return fmt.Errorf("variável de ambiente DATABASE_URL não encontrada")
	}

	var err error
	DB, err = sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("erro ao abrir conexão com banco: %v", err)
	}

	// Testa se a conexão realmente funciona
	if err = DB.Ping(); err != nil {
		return fmt.Errorf("erro ao pingar o banco de dados: %v", err)
	}

	fmt.Println("Conectado ao PostgreSQL com sucesso!")

	// Cria a tabela automaticamente caso não exista.
	// (Nota: Em projetos gigantes, usamos ferramentas de "migration" para isso,
	// mas para iniciar, isso aqui é perfeito e prático).
	createTableQuery := `
	CREATE TABLE IF NOT EXISTS google_tokens (
		matricula VARCHAR(50) PRIMARY KEY,
		access_token TEXT NOT NULL,
		refresh_token TEXT NOT NULL,
		token_type VARCHAR(50) NOT NULL,
		expiry TIMESTAMP NOT NULL
	);`

	_, err = DB.Exec(createTableQuery)
	if err != nil {
		return fmt.Errorf("erro ao criar tabela: %v", err)
	}

	return nil
}

// GetTokenFromDB busca o token de uma matrícula (Substitui o mock do classroom.go)
func GetTokenFromDB(matricula string) (*oauth2.Token, error) {
	var t oauth2.Token
	var expiry time.Time

	query := `SELECT access_token, refresh_token, token_type, expiry FROM google_tokens WHERE matricula = $1`

	err := DB.QueryRow(query, matricula).Scan(&t.AccessToken, &t.RefreshToken, &t.TokenType, &expiry)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("token não encontrado: o aluno ainda não fez login com o Google")
		}
		return nil, err
	}

	t.Expiry = expiry
	return &t, nil
}

// SaveToken salva o token após o fluxo de login OAuth2 do Google
// Se a matrícula já existir, ele atualiza (ideal para quando o token expira e é renovado)
func SaveToken(matricula string, token *oauth2.Token) error {
	query := `
	INSERT INTO google_tokens (matricula, access_token, refresh_token, token_type, expiry)
	VALUES ($1, $2, $3, $4, $5)
	ON CONFLICT (matricula) 
	DO UPDATE SET 
		access_token = EXCLUDED.access_token,
		refresh_token = EXCLUDED.refresh_token,
		token_type = EXCLUDED.token_type,
		expiry = EXCLUDED.expiry;`

	_, err := DB.Exec(query, matricula, token.AccessToken, token.RefreshToken, token.TokenType, token.Expiry)
	return err
}
