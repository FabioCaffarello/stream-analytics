package policykit

// NeverDropCloseFinal blocks drop decisions for close/final categories.
func NeverDropCloseFinal(category Category, decision Decision) bool {
	if category != CategoryCloseFinal {
		return false
	}
	return decision.HasAction(ActionDropDelta)
}
