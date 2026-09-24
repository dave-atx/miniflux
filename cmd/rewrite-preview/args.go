// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/scanner"

	"github.com/andybalholm/cascadia"
)

var quotedMatches = regexp.MustCompile(`:(matches|matchesOwn)\(\s*['"]`)

// checkRuleArgs catches problems the server accepts at save time but that make
// a rule silently do nothing at refresh: an invalid regex in replace() or
// replace_title(), or an invalid CSS selector in remove() / base64_decode().
// Tokenization matches rewrite.parseRules.
func checkRuleArgs(rulesText string) []string {
	var problems []string
	var name string
	argIndex := 0
	scan := scanner.Scanner{Mode: scanner.ScanIdents | scanner.ScanStrings}
	scan.Init(strings.NewReader(rulesText))
	scan.Error = func(*scanner.Scanner, string) {}
	for tok := scan.Scan(); tok != scanner.EOF; tok = scan.Scan() {
		switch tok {
		case scanner.Ident:
			name, argIndex = scan.TokenText(), 0
		case scanner.String:
			arg, err := strconv.Unquote(scan.TokenText())
			argIndex++
			if err != nil {
				continue
			}
			switch {
			case (name == "replace" || name == "replace_title") && argIndex == 1:
				if _, err := regexp.Compile(arg); err != nil {
					problems = append(problems, fmt.Sprintf("rewrite: %s regex %q does not compile: %v (rule would be a no-op)", name, arg, err))
				}
			case (name == "remove" || name == "base64_decode") && argIndex == 1:
				if _, err := cascadia.ParseGroup(arg); err != nil {
					problems = append(problems, fmt.Sprintf("rewrite: %s selector %q is invalid: %v (rule would be a no-op)", name, arg, err))
				} else if quotedMatches.MatchString(arg) {
					problems = append(problems, fmt.Sprintf("rewrite: %s selector %q quotes a :matches() regex; cascadia treats the quotes literally, so it never matches (write :matches(^foo$) unquoted)", name, arg))
				}
			}
		}
	}
	return problems
}
