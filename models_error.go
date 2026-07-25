package goai

import "strings"

type ModelsErrorCode string

const (
	ModelsErrorModelSource     ModelsErrorCode = "model_source"
	ModelsErrorModelValidation ModelsErrorCode = "model_validation"
	ModelsErrorProvider        ModelsErrorCode = "provider"
	ModelsErrorStream          ModelsErrorCode = "stream"
	ModelsErrorAuth            ModelsErrorCode = "auth"
	ModelsErrorOAuth           ModelsErrorCode = "oauth"
)

type ModelsError struct {
	Code  ModelsErrorCode
	Msg   string
	Cause error
}

func NewModelsError(code ModelsErrorCode, message string, cause error) *ModelsError {
	return &ModelsError{Code: code, Msg: message, Cause: cause}
}

func (e *ModelsError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil || e.Cause.Error() == "" || strings.Contains(e.Msg, e.Cause.Error()) {
		return e.Msg
	}
	return e.Msg + ": " + e.Cause.Error()
}

func (e *ModelsError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
