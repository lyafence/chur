package provider

// validProviders contains the set of provider names that have an implementation.
// Add new providers here when their package registers with the factory.
var validProviders = map[string]struct{}{
	"env":   {},
	"local": {},
	"k8s":   {},
}

// IsValidName reports whether name is a known, registered provider implementation.
func IsValidName(name string) bool {
	_, ok := validProviders[name]
	return ok
}
