package handler

// ErrorResponse is the standard error envelope returned on all non-2xx responses.
type ErrorResponse struct {
	Error string `json:"error"`
}
