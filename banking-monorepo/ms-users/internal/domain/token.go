package domain

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	UserID       string
	Balance      *string
}
