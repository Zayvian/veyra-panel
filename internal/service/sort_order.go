package service

import (
	"strconv"
	"strings"
)

func checkSortOrder(value int) error {
	if value < 0 || value > 999999 {
		return invalid("订阅排序必须是 0–999999 的整数，数字越小越靠前")
	}
	return nil
}

func ParseSortOrder(value string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, invalid("订阅排序必须是 0–999999 的整数")
	}
	return n, checkSortOrder(n)
}
