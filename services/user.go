package services

import (
	"errors"
	"video-streaming-server/repositories"
	"video-streaming-server/types"

	"github.com/google/uuid"
)

type UserService interface {
	RegisterUser(username, email, password string) (*types.User, error)
	AuthenticateUser(email, password string) (*types.User, error)
	GetUserByEmail(email string) (*types.User, error)
	GetUserByUsername(username string) (*types.User, error)
	GetUserByID(id string) (*types.User, error)
	VerifyUser(id string) error
}

type userService struct {
	repository repositories.UserRepository
}

func NewUserService(repository repositories.UserRepository) UserService {
	return &userService{repository: repository}
}

func (s *userService) GetUserByEmail(email string) (*types.User, error) {
	return s.repository.GetUserByEmail(email)
}

func (s *userService) GetUserByUsername(username string) (*types.User, error) {
	return s.repository.GetUserByUsername(username)
}

func (s *userService) GetUserByID(id string) (*types.User, error) {
	return s.repository.GetUserByID(id)
}

func (s *userService) RegisterUser(username, email, password string) (*types.User, error) {
	existingUser, err := s.GetUserByEmail(email)
	if err == nil && existingUser != nil {
		return nil, errors.New("email already exists")
	}

	existingUser, err = s.GetUserByUsername(username)
	if err == nil && existingUser != nil {
		return nil, errors.New("username already exists")
	}

	newUser, _ := types.NewUser(username, email, password)
	newUser.ID = uuid.New().String()

	err = s.repository.CreateUser(newUser)
	if err != nil {
		return nil, err
	}

	return newUser, nil
}

func (s *userService) AuthenticateUser(email, password string) (*types.User, error) {

	user, err := s.repository.GetUserByEmail(email)
	if err != nil {
		return nil, errors.New("invalid credentials")
	}

	if user == nil {
		return nil, errors.New("user does not exist")
	}

	if !user.ComparePassword(password) {
		return nil, errors.New("invalid credentials")
	}

	return user, nil
}

func (s *userService) VerifyUser(id string) error {
	return s.repository.VerifyUser(id)
}
