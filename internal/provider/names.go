package provider

// IsValidName reports whether name is a known, registered provider implementation.
func IsValidName(name string) bool {
	_, ok := Get(name)
	return ok
}
