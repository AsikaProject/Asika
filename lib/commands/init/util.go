package init

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

func StepHeader(num int, title string) {
	fmt.Printf("\n─── Step %d: %s ───\n\n", num, title)
}

func Prompt(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("  %s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("  %s: ", label)
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func PromptSecret(reader *bufio.Reader, label string) string {
	fmt.Printf("  %s: ", label)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

func Choose(reader *bufio.Reader, label string, options []string, defaultVal string) string {
	fmt.Printf("  %s\n", label)
	for i, opt := range options {
		marker := " "
		if opt == defaultVal {
			marker = "*"
		}
		fmt.Printf("    %s [%d] %s\n", marker, i+1, opt)
	}
	fmt.Printf("  Enter choice [%d]: ", indexOf(options, defaultVal)+1)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	idx, err := strconv.Atoi(input)
	if err == nil && idx >= 1 && idx <= len(options) {
		return options[idx-1]
	}
	return input
}

func Confirm(reader *bufio.Reader, label string) bool {
	fmt.Printf("  %s (y/n) [n]: ", label)
	input, _ := reader.ReadString('\n')
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "y" || input == "yes"
}

func SplitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func indexOf(slice []string, val string) int {
	for i, s := range slice {
		if s == val {
			return i
		}
	}
	return 0
}
