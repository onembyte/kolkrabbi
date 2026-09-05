package auth

func ValidateName(n string) bool { return n != "" }

func Rename(s *Session, n string) bool { return ValidateName(n) }
