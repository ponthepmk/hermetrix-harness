package product

// ResolveProjectRootBinding canonicalizes an existing local project root using
// the same confinement boundary as the workspace service. It performs no write.
func ResolveProjectRootBinding(root string) (string, error) {
	return resolveProjectRoot(root)
}
