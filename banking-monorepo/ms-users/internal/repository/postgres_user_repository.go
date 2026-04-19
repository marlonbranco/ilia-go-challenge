package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ms-users/internal/domain"
	
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresUserRepository struct {
	pool *pgxpool.Pool
}

const (
	postgresDuplicateKeyErrorCode = "23505"

	createUserQuery = `
		INSERT INTO users (id, first_name, last_name, email, password_hash, is_deleted, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, first_name, last_name, email, password_hash, is_deleted, created_at, updated_at
	`
	findByEmailQuery = `
		SELECT id, first_name, last_name, email, password_hash, is_deleted, created_at, updated_at
		FROM users
		WHERE email = $1 AND is_deleted = false
	`
	findByIdQuery = `
		SELECT id, first_name, last_name, email, password_hash, is_deleted, created_at, updated_at
		FROM users
		WHERE id = $1 AND is_deleted = false
	`
	updateUserQuery = `
		UPDATE users
		SET first_name = $1, last_name = $2, email = $3, password_hash = $4, updated_at = $5
		WHERE id = $6 AND is_deleted = false
	`
	findAllQuery = `
		SELECT id, first_name, last_name, email, password_hash, is_deleted, created_at, updated_at
		FROM users
		WHERE is_deleted = false
	`
	deleteUserQuery = `
		UPDATE users
		SET is_deleted = true, updated_at = $1
		WHERE id = $2
	`

	errDuplicateEmailMsg = "duplicate email: %w"
	errUserNotFoundMsg   = "user not found: %w"
	errFailedToCreateMsg = "failed to create user"
	errFailedToFindMsg   = "failed to find user"
	errFailedToUpdateMsg = "failed to update user"
	errFailedToDeleteMsg = "failed to delete user"
)

func NewPostgresUserRepository(ctx context.Context, connectionUrl string, migrationsUrl string) (*PostgresUserRepository, error) {
	config, err := pgxpool.ParseConfig(connectionUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection url: %w", err)
	}

	config.MaxConns = 25
	config.MinConns = 5
	config.MaxConnLifetime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	migrations, err := migrate.New(migrationsUrl, connectionUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to create migrate instance: %w", err)
	}

	if err := migrations.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return &PostgresUserRepository{pool: pool}, nil
}

func (userRepository *PostgresUserRepository) Close() {
	userRepository.pool.Close()
}

func (userRepository *PostgresUserRepository) Create(ctx context.Context, user domain.User) (domain.User, error) {
	err := userRepository.pool.QueryRow(ctx, createUserQuery,
		user.ID,
		user.FirstName,
		user.LastName,
		user.Email,
		user.PasswordHash,
		user.IsDeleted,
		user.CreatedAt,
		user.UpdatedAt,
	).Scan(
		&user.ID,
		&user.FirstName,
		&user.LastName,
		&user.Email,
		&user.PasswordHash,
		&user.IsDeleted,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == postgresDuplicateKeyErrorCode {
			return domain.User{}, fmt.Errorf(errDuplicateEmailMsg, domain.ErrDuplicate)
		}
		return domain.User{}, errors.New(errFailedToCreateMsg)
	}

	return user, nil
}

func (userRepository *PostgresUserRepository) FindAll(ctx context.Context) ([]domain.User, error) {
	rows, err := userRepository.pool.Query(ctx, findAllQuery)
	if err != nil {
		return nil, errors.New(errFailedToFindMsg)
	}
	defer rows.Close()

	var users []domain.User
	for rows.Next() {
		var user domain.User
		err := rows.Scan(
			&user.ID,
			&user.FirstName,
			&user.LastName,
			&user.Email,
			&user.PasswordHash,
			&user.IsDeleted,
			&user.CreatedAt,
			&user.UpdatedAt,
		)
		if err != nil {
			return nil, errors.New(errFailedToFindMsg)
		}
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.New(errFailedToFindMsg)
	}

	return users, nil
}

func (userRepository *PostgresUserRepository) FindByEmail(ctx context.Context, email string) (domain.User, error) {
	var user domain.User
	err := userRepository.pool.QueryRow(ctx, findByEmailQuery, email).Scan(
		&user.ID,
		&user.FirstName,
		&user.LastName,
		&user.Email,
		&user.PasswordHash,
		&user.IsDeleted,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, fmt.Errorf(errUserNotFoundMsg, domain.ErrNotFound)
		}
		return domain.User{}, errors.New(errFailedToFindMsg)
	}

	return user, nil
}

func (userRepository *PostgresUserRepository) FindByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	var user domain.User
	err := userRepository.pool.QueryRow(ctx, findByIdQuery, id).Scan(
		&user.ID,
		&user.FirstName,
		&user.LastName,
		&user.Email,
		&user.PasswordHash,
		&user.IsDeleted,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, fmt.Errorf(errUserNotFoundMsg, domain.ErrNotFound)
		}
		return domain.User{}, errors.New(errFailedToFindMsg)
	}

	return user, nil
}

func (userRepository *PostgresUserRepository) Update(ctx context.Context, user domain.User) error {
	user.UpdatedAt = time.Now().UTC()

	commandTag, err := userRepository.pool.Exec(ctx, updateUserQuery,
		user.FirstName,
		user.LastName,
		user.Email,
		user.PasswordHash,
		user.UpdatedAt,
		user.ID,
	)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == postgresDuplicateKeyErrorCode {
			return fmt.Errorf(errDuplicateEmailMsg, domain.ErrDuplicate)
		}
		return errors.New(errFailedToUpdateMsg)
	}

	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("%w: user with id %s not found", domain.ErrNotFound, user.ID)
	}

	return nil
}

func (userRepository *PostgresUserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	updatedAt := time.Now().UTC()
	commandTag, err := userRepository.pool.Exec(ctx, deleteUserQuery, updatedAt, id)
	if err != nil {
		return errors.New(errFailedToDeleteMsg)
	}

	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("%w: user with id %s not found", domain.ErrNotFound, id)
	}

	return nil
}
