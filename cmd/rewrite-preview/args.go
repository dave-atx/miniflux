// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/scanner"
)

var quotedMatches = regexp.MustCompile(`:(matches|matchesOwn)\(\s*['"]`)

// lintRuleArgs catches selectors that are valid, so the server saves them, but
// almost certainly don't do what was meant. cascadia keeps the quotes in
// :matches("x") as part of the regex, so the selector never matches. Invalid
// regexes and selectors are rejected by rewrite.ValidateRules instead.
// Tokenization matches rewrite.parseRules.
func lintRuleArgs(rulesText string) []string {
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
			if err != nil || argIndex != 1 || (name != "remove" && name != "base64_decode") {
				continue
			}
			if quotedMatches.MatchString(arg) {
				problems = append(problems, fmt.Sprintf("rewrite: %s selector %q quotes a :matches() regex; cascadia treats the quotes literally, so it never matches (write :matches(^foo$) unquoted)", name, arg))
			}
		}
	}
	return problems
}
