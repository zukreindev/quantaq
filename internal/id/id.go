package id

import (
	"github.com/google/uuid"
)

func GetID() string {
	newUUID, _ := uuid.NewUUID()
	return newUUID.String()
}

