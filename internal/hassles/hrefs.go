package hassles

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
)

// go-webdav discards origin/query information when exposing decoded href paths.
// Validate DAV hrefs at the raw response boundary, before that information is lost.
func validateResponseHrefs(body []byte, request *http.Request) error {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	depth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errors.New("invalid calendar XML response")
		}
		switch element := token.(type) {
		case xml.StartElement:
			depth++
			if depth > 128 {
				return errors.New("calendar XML nesting exceeds safety limit")
			}
			if element.Name.Space != "DAV:" || element.Name.Local != "href" {
				continue
			}
			var href string
			if err := decoder.DecodeElement(&href, &element); err != nil {
				return errors.New("invalid calendar href element")
			}
			depth--
			if _, err := canonical(href); err != nil {
				return err
			}
			if request.Method == "REPORT" {
				if _, err := Resource(request.URL.Path, href); err != nil {
					return err
				}
			}
		case xml.EndElement:
			depth--
		}
	}
}
