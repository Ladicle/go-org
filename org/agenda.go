package org

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Agenda struct {
	Logs map[string]Timestamp
}

var agendaRegexp = regexp.MustCompile(`(CLOSED|DEADLINE|SCHEDULED):\s(?:\[|<)(\d{4}-\d{2}-\d{2})( [A-Za-z]+)?( \d{2}:\d{2})?( \+\d+[dwmy])?(?:]|>)`)

func lexAgenda(line string) (token, bool) {
	m := agendaRegexp.FindAllStringSubmatch(line, 3)
	if len(m) > 0 {
		var ret []string
		for _, v := range m {
			ret = append(ret, v[1:]...)
		}
		return token{"agenda", len(m), "", ret}, true
	}
	return nilToken, false
}

func (d *Document) parseAgenda(i int, stop stopFn) (int, Node) {
	var (
		token  = d.tokens[i]
		agenda = Agenda{Logs: make(map[string]Timestamp, token.lvl)}
	)

	for j := 0; j < token.lvl; j++ {
		idx := j * 5
		key := strings.ToUpper(token.matches[idx])

		// time format
		if token.matches[idx+3] != "" {
			t, err := time.Parse(timestampFormat, fmt.Sprintf("%s%s%s",
				token.matches[idx+1], token.matches[idx+2], token.matches[idx+3]))
			if err != nil {
				return 0, nil
			}

			agenda.Logs[key] = Timestamp{t, false, strings.TrimSpace(token.matches[idx+4])}
			continue
		}
		// date format
		t, err := time.Parse(datestampFormat, fmt.Sprintf("%s%s",
			token.matches[idx+1], token.matches[idx+2]))
		if err != nil {
			return 0, nil
		}
		agenda.Logs[key] = Timestamp{t, true, strings.TrimSpace(token.matches[idx+4])}
	}
	return 1, agenda
}

func (n Agenda) String() string { return orgWriter.WriteNodesAsString(n) }
