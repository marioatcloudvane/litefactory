package messaging

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

var errPageBudget = errors.New("report too large, narrow the filter")

// replySize counts UTF-16 units, which also bounds transports counting runes.
func replySize(text string) int { return len(utf16.Encode([]rune(text))) }

func shortenLine(line string, limit int) string {
	const mark = " … [shortened]"
	if replySize(line) <= limit {
		return line
	}
	if limit < replySize(mark) {
		return ""
	}
	var b strings.Builder
	used := 0
	for _, r := range line {
		size := 1
		if r > 0xffff {
			size = 2
		}
		if used+size+replySize(mark) > limit {
			break
		}
		b.WriteRune(r)
		used += size
	}
	return b.String() + mark
}

// buildPages reserves the complete coverage block and footer before placing rows.
// No report state survives the command. Coverage is repeated so every page is
// meaningful when read on its own.
func buildPages(summary, rows []string, limit int, at time.Time, next func(int) string) ([]string, error) {
	if limit <= 0 {
		return nil, errPageBudget
	}
	header := strings.Join(summary, "\n")
	if header != "" {
		header += "\n"
	}
	stamp := at.UTC().Format(time.RFC3339)
	footer := func(page, count int) string {
		text := fmt.Sprintf("page %d of %d, as of %s", page, count, stamp)
		if page < count {
			text += "\nnext: " + next(page+1)
		}
		return text
	}
	// Use the largest possible page count, bounded by the number of input lines.
	// This removes the circular dependency between footer width and pagination.
	var wholeLines []string
	for _, row := range rows {
		wholeLines = append(wholeLines, strings.Split(row, "\n")...)
	}
	countBound := len(wholeLines) + 1
	reserve := replySize(footer(countBound-1, countBound))
	reserve = max(reserve, replySize(footer(1, countBound)))
	budget := limit - replySize(header) - reserve - 1
	if budget < replySize(" … [shortened]") {
		return nil, errPageBudget
	}
	chunks := []string{}
	var lines []string
	used := 0
	for _, line := range wholeLines {
		line = shortenLine(line, budget)
		size := replySize(line)
		if len(lines) > 0 && used+1+size > budget {
			chunks = append(chunks, strings.Join(lines, "\n"))
			lines = nil
			used = 0
		}
		if len(lines) > 0 {
			used++
		}
		lines = append(lines, line)
		used += size
	}
	if len(lines) > 0 || len(chunks) == 0 {
		chunks = append(chunks, strings.Join(lines, "\n"))
	}
	pages := make([]string, len(chunks))
	for i, chunk := range chunks {
		text := header + chunk
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		pages[i] = text + footer(i+1, len(chunks))
		if replySize(pages[i]) > limit {
			return nil, errPageBudget
		}
	}
	return pages, nil
}

func requestedPage(args map[string]string) (int, error) {
	if args["page"] == "" {
		return 1, nil
	}
	page, err := strconv.Atoi(args["page"])
	if err != nil || page < 1 {
		return 0, errors.New("page must be a positive whole number")
	}
	return page, nil
}

func pageReply(pages []string, page int) string {
	if page > len(pages) {
		return fmt.Sprintf("page outside range; last valid page is %d", len(pages))
	}
	return pages[page-1]
}

// nextCommand preserves every filter using slash-command name:value syntax.
func nextCommand(command string, args map[string]string, names []string, page int) string {
	var b strings.Builder
	b.WriteString("/" + command)
	for _, name := range names {
		value, ok := args[name]
		if !ok || value == "" || name == "page" {
			continue
		}
		b.WriteString(" " + name + ":")
		if strings.ContainsAny(value, " \t\r\n\"\\") {
			b.WriteString(strconv.Quote(value))
		} else {
			b.WriteString(value)
		}
	}
	b.WriteString(" page:" + strconv.Itoa(page))
	return b.String()
}
