package protection

import "log"

func safeLog(level string) { log.Printf("temporal_sdk level=%s details=withheld", level) }
