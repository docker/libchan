package libchan

type Message {
	Data	[]byte
	Fd	*os.File
	Ret	Sender
}
