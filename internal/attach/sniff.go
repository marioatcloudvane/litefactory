package attach

import (
	"archive/zip"
	"fmt"
	"net/http"
	"strings"
)

// sniffLen is how many leading bytes [sniff] needs. net/http's detector
// never looks past 512, so reading more buys nothing and reading fewer can
// misclassify a format whose magic number sits late in that window.
const sniffLen = 512

// Kind is the family an attachment belongs to, which is what decides the
// ceiling it is measured against and how the prompt describes it.
type Kind int

const (
	// KindUnsupported is anything litefactory does not accept.
	KindUnsupported Kind = iota
	// KindImage is a PNG, JPEG, GIF or WebP: something the agent can look
	// at directly.
	KindImage
	// KindDocument is a PDF, DOCX or PPTX: something the agent has to open
	// or convert with its own tooling (US-4).
	KindDocument
)

// supportedImages maps a sniffed image media type to the file extension
// litefactory lands it under.
//
// The extension comes from this table rather than from the attachment's
// declared name, so a PNG uploaded as "diagram.jpg" lands as a .png and an
// agent's image loader is not lied to about what it is opening.
var supportedImages = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// Media types and extensions for the three document formats US-4 admits.
const (
	mediaPDF  = "application/pdf"
	mediaDOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	mediaPPTX = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
)

var documentExt = map[string]string{
	mediaPDF:  ".pdf",
	mediaDOCX: ".docx",
	mediaPPTX: ".pptx",
}

// zipMediaType is what net/http's sniffer calls every ZIP container,
// including both OOXML formats this package accepts. Telling a .docx from a
// .pptx from an ordinary archive is [classifyZip]'s job, and it cannot be
// done from the first 512 bytes — see there.
const zipMediaType = "application/zip"

// sniff decides what an attachment is from its leading bytes, and reports
// the media type, the extension to land it under, and its [Kind].
//
// The bytes are authoritative (AC-3.5). Neither the Content-Type the forge
// declared nor the extension on the attachment's name is consulted, because
// both are attacker-supplied in the threat model this feature lives in: the
// person who attaches the file also controls what it claims to be. Sniffing
// is the only one of the three that reads what is really there.
//
// A ZIP comes back as [KindDocument] with the [zipMediaType] media type and
// no extension. That is provisional: it means "this may be a DOCX or a PPTX,
// keep reading", and the caller must settle it with [classifyZip] once the
// whole file has landed. Everything else sniff returns is final.
func sniff(head []byte) (mediaType, ext string, kind Kind) {
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

	if ext, ok := supportedImages[detected]; ok {
		return detected, ext, KindImage
	}
	if detected == mediaPDF {
		return detected, documentExt[mediaPDF], KindDocument
	}
	if detected == zipMediaType {
		return zipMediaType, "", KindDocument
	}
	return detected, "", KindUnsupported
}

// classifyZip settles what a landed ZIP container actually is by reading its
// entry names.
//
// It exists because the two OOXML formats cannot be told apart by magic
// number: a .docx and a .pptx are both ordinary ZIP archives beginning
// "PK\x03\x04", and so is a holiday-photos.zip. What distinguishes them is
// the part inside — word/document.xml for a document, ppt/presentation.xml
// for a presentation — and in a real Office file that name sits well past
// the 512 bytes [sniff] gets to look at, because [Content_Types].xml is
// stored first and is larger than that on its own.
//
// So the file is landed first and classified after. That keeps "the bytes
// decide" true for these formats too: a plain archive renamed to .docx is
// refused here, exactly as a PDF renamed to .png is refused by sniff. The
// cost is that an unsupported archive is written to disk before it is
// rejected, which is why the caller removes it — the same removal path an
// oversized file takes.
//
// Reading is bounded by the file's own size, which the caller has already
// held under the document ceiling, so this cannot be made to read more than
// that ceiling allows. archive/zip is stdlib: US-4 admits no parsing
// dependency, and this is not one.
func classifyZip(r *zip.Reader) (mediaType, ext string, ok bool) {
	var hasWord, hasPPT bool
	for _, f := range r.File {
		switch {
		case strings.HasPrefix(f.Name, "word/"):
			hasWord = true
		case strings.HasPrefix(f.Name, "ppt/"):
			hasPPT = true
		}
	}
	switch {
	// Both would be a malformed or deliberately confusing container.
	// Refusing is the honest answer: there is no way to say which
	// application should open it, and guessing would be picking one at
	// random.
	case hasWord && hasPPT:
		return zipMediaType, "", false
	case hasWord:
		return mediaDOCX, documentExt[mediaDOCX], true
	case hasPPT:
		return mediaPPTX, documentExt[mediaPPTX], true
	default:
		return zipMediaType, "", false
	}
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
