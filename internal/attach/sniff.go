package attach

import (
	"fmt"
	"net/http"
	"strings"
)

// sniffLen is how many leading bytes [sniff] needs. net/http's detector
// never looks past 512, so reading more buys nothing and reading fewer can
// misclassify a format whose magic number sits late in that window.
const sniffLen = 512

// supportedImages maps a sniffed image media type to the file extension
// litefactory lands it under.
//
// The extension comes from this table rather than from the attachment's
// declared name, so a PNG uploaded as "diagram.jpg" lands as a .png and an
// agent's image loader is not lied to about what it is opening.
//
// This is US-3's scope: images. Documents (PDF, DOCX, PPTX) are US-4, and
// until that story lands they sniff to no entry here and are declined as
// [ReasonUnsupportedType] — which is the correct answer today, because
// nothing downstream would know what to do with them.
var supportedImages = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// sniff decides what an attachment actually is from its leading bytes, and
// reports the media type and the extension to land it under.
//
// The bytes are authoritative (AC-3.5). Neither the Content-Type the forge
// declared nor the extension on the attachment's name is consulted, because
// both are attacker-supplied in the threat model this feature lives in: the
// person who attaches the file also controls what it claims to be. Sniffing
// is the only one of the three that reads what is really there.
//
// ok is false for anything not in [supportedImages], including a type that
// is a perfectly real image format litefactory has simply not agreed to
// handle. The caller turns that into a [ReasonUnsupportedType] rejection.
func sniff(head []byte) (mediaType, ext string, ok bool) {
	// DetectContentType always returns something — "application/octet-stream"
	// when it recognises nothing — and never errors, so there is no failure
	// mode here beyond "not a type we support".
	detected := http.DetectContentType(head)

	// The detector returns a full header value, so "image/gif" arrives as
	// "image/gif" but "text/plain" arrives as "text/plain; charset=utf-8".
	// Only the media type matters for the lookup.
	if i := strings.IndexByte(detected, ';'); i >= 0 {
		detected = detected[:i]
	}
	detected = strings.ToLower(strings.TrimSpace(detected))

	ext, ok = supportedImages[detected]
	return detected, ext, ok
}

// unsupportedDetail composes the [Rejection.Detail] for a sniffed type
// litefactory does not accept.
//
// It states litefactory's own classification of the bytes, which is safe to
// echo: it is one of a fixed set of media-type strings net/http can return,
// never text that arrived from the forge. The attachment's declared type and
// name are deliberately absent — quoting back what the uploader claimed
// would put third-party text in a comment for no diagnostic gain, since the
// whole point is that the claim was not believed.
func unsupportedDetail(mediaType string) string {
	return fmt.Sprintf("sniffed as %s", mediaType)
}
