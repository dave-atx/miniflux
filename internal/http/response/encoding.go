// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package response

import (
	"slices"
	"strconv"
	"strings"
)

type acceptEncodingParser struct {
	// accepted contains all encoding that particular parser instance advertises.
	accepted []string
}

// AcceptEncoding creates parser instance for "Accept-Encoding" header values.
// It accepts list of encodings recognized by user of this parser instance.
func AcceptEncoding(accepted ...string) *acceptEncodingParser {
	return &acceptEncodingParser{accepted: accepted}
}

// Parse the input string according to [HTTP Semantics] and returns our
// most-preferred encoding that the client accepts. Candidates are matched in
// the order of the accepted list passed to [AcceptEncoding], so the server's
// preference wins over the order (and weights) the client advertises.
//
// Currently this function ignores set weights other than q=0.
// Encodings with q=0 will not be considered.
//
// Returns "identity" if the input is empty or no accepted encoding matches.
//
// [HTTP Semantics]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Accept-Encoding.
func (p *acceptEncodingParser) Parse(acceptEncoding string) string {
	candidates := make([]string, 0, 5)

	for enc := range strings.SplitSeq(acceptEncoding, ",") {
		enc = strings.TrimSpace(enc)

		if qi := strings.IndexByte(enc, ';'); qi > -1 {
			qstr := strings.TrimPrefix(enc[qi:], ";")
			qstr = strings.TrimSpace(qstr)
			qstr = strings.TrimPrefix(qstr, "q=")

			q, err := strconv.ParseFloat(qstr, 64)
			if err != nil || q == 0 {
				continue // Ignore weird float values.
			}
			enc = strings.TrimSpace(enc[:qi])
		}

		candidates = append(candidates, enc)
	}

	for _, accepted := range p.accepted {
		if slices.Contains(candidates, accepted) {
			return accepted
		}
	}

	return "identity"
}
