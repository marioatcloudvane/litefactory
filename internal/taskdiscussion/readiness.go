package taskdiscussion

// PublicationAvailable reports whether a separately authorized publisher is installed.
func (s *Service) PublicationAvailable() bool { return s != nil && s.publisher != nil }
