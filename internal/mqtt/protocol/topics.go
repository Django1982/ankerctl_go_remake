package protocol

import "fmt"

// TopicNotice returns /phone/maker/{sn}/notice.
func TopicNotice(sn string) string {
	return fmt.Sprintf("/phone/maker/%s/notice", sn)
}

// TopicCommandReply returns /phone/maker/{sn}/command/reply.
func TopicCommandReply(sn string) string {
	return fmt.Sprintf("/phone/maker/%s/command/reply", sn)
}

// TopicQueryReply returns /phone/maker/{sn}/query/reply.
func TopicQueryReply(sn string) string {
	return fmt.Sprintf("/phone/maker/%s/query/reply", sn)
}

// TopicCommand returns /device/maker/{sn}/command.
func TopicCommand(sn string) string {
	return fmt.Sprintf("/device/maker/%s/command", sn)
}

// TopicQuery returns /device/maker/{sn}/query.
func TopicQuery(sn string) string {
	return fmt.Sprintf("/device/maker/%s/query", sn)
}
