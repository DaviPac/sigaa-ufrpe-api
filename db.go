package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"sync/atomic"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/oauth2"
)

// DB é a conexão global com o banco de dados. Pode ser nil se o banco
// não estiver configurado; use dbReady() antes de tocar nele.
var DB *sql.DB

// dbHealthy indica se a última verificação de conexão teve sucesso.
// É atualizado pelo watcher em background, então o servidor continua
// de pé mesmo que o Postgres esteja fora do ar na inicialização.
var dbHealthy atomic.Bool

// migrationDone marca que o CREATE TABLE já rodou com sucesso.
var migrationDone atomic.Bool

// ErrDBUnavailable é retornado quando uma rota depende do banco e ele está fora.
var ErrDBUnavailable = errors.New("banco de dados indisponível no momento")

const createTokensTable = `
CREATE TABLE IF NOT EXISTS google_tokens (
	matricula     VARCHAR(50) PRIMARY KEY,
	access_token  TEXT NOT NULL,
	refresh_token TEXT NOT NULL,
	token_type    VARCHAR(50) NOT NULL,
	expiry        TIMESTAMP NOT NULL
);`

// InitDB abre o pool de conexões e dispara um watcher que mantém o
// estado de saúde atualizado. Nunca aborta o processo: se o banco
// estiver indisponível, apenas as rotas de /classroom ficam degradadas.
func InitDB(connStr string) {
	if connStr == "" {
		log.Println("⚠️  DATABASE_URL não definida — rotas /classroom ficarão indisponíveis")
		return
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Printf("⚠️  erro ao abrir conexão com o banco: %v (rotas /classroom indisponíveis)", err)
		return
	}

	// Pool enxuto: esta API faz pouquíssimas queries.
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	DB = db

	// Primeira checagem síncrona (rápida) só para logar o estado inicial.
	if err := pingAndMigrate(3 * time.Second); err != nil {
		log.Printf("⚠️  banco ainda indisponível: %v — tentando reconectar em background", err)
	} else {
		log.Println("✅ Conectado ao PostgreSQL com sucesso!")
	}

	go dbHealthWatcher()
}

// dbHealthWatcher revalida a conexão periodicamente para que o serviço
// se recupere sozinho quando o banco voltar.
func dbHealthWatcher() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := pingAndMigrate(3 * time.Second); err != nil {
			if dbHealthy.Swap(false) {
				log.Printf("⚠️  conexão com o banco perdida: %v", err)
			}
		} else if !dbHealthy.Swap(true) {
			log.Println("✅ conexão com o banco restabelecida")
		}
	}
}

func pingAndMigrate(timeout time.Duration) error {
	if DB == nil {
		return ErrDBUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := DB.PingContext(ctx); err != nil {
		return err
	}

	if !migrationDone.Load() {
		if _, err := DB.ExecContext(ctx, createTokensTable); err != nil {
			return err
		}
		migrationDone.Store(true)
	}

	dbHealthy.Store(true)
	return nil
}

// dbReady informa se dá para consultar o banco agora.
func dbReady() bool {
	return DB != nil && dbHealthy.Load()
}

// CloseDB fecha o pool com segurança (no-op se nunca foi aberto).
func CloseDB() {
	if DB != nil {
		_ = DB.Close()
	}
}

// GetTokenFromDB busca o token OAuth de uma matrícula.
func GetTokenFromDB(matricula string) (*oauth2.Token, error) {
	if !dbReady() {
		return nil, ErrDBUnavailable
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var (
		t      oauth2.Token
		expiry time.Time
	)
	const query = `SELECT access_token, refresh_token, token_type, expiry FROM google_tokens WHERE matricula = $1`
	err := DB.QueryRowContext(ctx, query, matricula).
		Scan(&t.AccessToken, &t.RefreshToken, &t.TokenType, &expiry)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("token não encontrado: o aluno ainda não fez login com o Google")
		}
		return nil, err
	}

	t.Expiry = expiry
	return &t, nil
}

// SaveToken insere ou atualiza o token OAuth de uma matrícula.
func SaveToken(matricula string, token *oauth2.Token) error {
	if !dbReady() {
		return ErrDBUnavailable
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const query = `
	INSERT INTO google_tokens (matricula, access_token, refresh_token, token_type, expiry)
	VALUES ($1, $2, $3, $4, $5)
	ON CONFLICT (matricula)
	DO UPDATE SET
		access_token  = EXCLUDED.access_token,
		refresh_token = EXCLUDED.refresh_token,
		token_type    = EXCLUDED.token_type,
		expiry        = EXCLUDED.expiry;`

	_, err := DB.ExecContext(ctx, query, matricula, token.AccessToken, token.RefreshToken, token.TokenType, token.Expiry)
	return err
}
