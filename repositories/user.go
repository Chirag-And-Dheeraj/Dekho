package repositories

import (
	"database/sql"
	"log"
	"video-streaming-server/types"
)

type UserRepository interface {
	CreateUser(user *types.User) error
	GetUserByEmail(email string) (*types.User, error)
	GetUserByUsername(username string) (*types.User, error)
	GetUserByID(id string) (*types.User, error)
	VerifyUser(id string) error
}

type userRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) CreateUser(user *types.User) error {
	_, err := r.db.Exec(`
		INSERT INTO users (id, username, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, user.ID, user.Username, user.Email, user.HashedPassword, user.CreatedAt, user.UpdatedAt)

	return err
}

func (r *userRepository) GetUserByEmail(email string) (*types.User, error) {
	var user types.User
	err := r.db.QueryRow(`
		SELECT id, username, email, password_hash, created_at, updated_at
		FROM users WHERE email = $1
	`, email).Scan(&user.ID, &user.Username, &user.Email, &user.HashedPassword, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			log.Println("no rows")
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) GetUserByUsername(username string) (*types.User, error) {
	var user types.User
	err := r.db.QueryRow(`
		SELECT id, username, email, password_hash, created_at, updated_at
		FROM users WHERE username = $1
	`, username).Scan(&user.ID, &user.Username, &user.Email, &user.HashedPassword, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) GetUserByID(id string) (*types.User, error) {
	var user types.User
	err := r.db.QueryRow(`
		SELECT id, username, email, password_hash, created_at, updated_at
		FROM users WHERE id = $1
	`, id).Scan(&user.ID, &user.Username, &user.Email, &user.HashedPassword, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) VerifyUser(id string) error {
	_, err := r.db.Exec(`UPDATE users SET is_verified = TRUE WHERE id = $1`, id)
	if err != nil {
		return err
	}

	return nil
}
