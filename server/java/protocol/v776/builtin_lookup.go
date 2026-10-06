package v776

// BuiltinID is the protocol id of an entry in a built-in registry, or -1.
func BuiltinID(registry, entry string) int32 {
	if id, ok := Builtin[registry][entry]; ok {
		return id
	}
	return -1
}
