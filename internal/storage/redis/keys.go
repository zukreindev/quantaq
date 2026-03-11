package redis

import "fmt"

func WaitingKey(name string) string {
	return fmt.Sprintf("queue:%s:waiting", name)
}

func ProcessingKey(name string) string {
	return fmt.Sprintf("queue:%s:processing", name)
}

func FailedKey(name string) string {
	return fmt.Sprintf("queue:%s:failed", name)
}

func JobKey(id string) string {
	return fmt.Sprintf("job:%s", id)
}
