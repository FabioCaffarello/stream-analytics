package policykit

// DropAllowed reports whether DropDelta can remove an event from this category.
// Close/final is never drop-eligible.
func DropAllowed(category Category, decision Decision) bool {
	if !decision.HasAction(ActionDropDelta) {
		return false
	}
	if category == CategoryCloseFinal {
		return false
	}
	return category == CategoryDelta
}
