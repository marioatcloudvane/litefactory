package turn

// testValidateLayout is a report layout with a fixed overview and the
// finding command reference of st, as RefreshValidateReport builds it.
func testValidateLayout(overview string, st ValidateState) validateReportLayout {
	return validateReportLayout{Summary: overview, Header: validateGuidanceHeader(st), Commands: validateDecisionItemsFor(false, st)}
}
