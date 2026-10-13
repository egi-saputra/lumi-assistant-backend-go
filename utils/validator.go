package utils

import "github.com/go-playground/validator/v10"

func FormatValidationError(err error) []string {
	var errors []string

	validationErrs, ok := err.(validator.ValidationErrors)
	if !ok {
		return []string{"Invalid request"}
	}

	for _, e := range validationErrs {
		switch e.Field() {
		case "EmailOrPhone":
			errors = append(errors, "Please enter your email, email or phone is required")
		case "Password":
			errors = append(errors, "Please enter the password, password is required")
		default:
			errors = append(errors, e.Field()+" is required")
		}
	}

	return errors
}