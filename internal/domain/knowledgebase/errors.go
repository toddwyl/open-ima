package knowledgebase

import "errors"

var (
	ErrNameTaken = errors.New("knowledge base name already taken")
	ErrNotFound  = errors.New("knowledge base not found")
)
