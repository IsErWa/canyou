package bandwidth

import (
	"io"
	"net/http"
)

type bodyReader struct {
	io.ReadCloser
	transfer *Transfer
}

// Reader 必须放在 multipart 解析之前；Close 仍关闭原始请求体。
func (transfer *Transfer) Reader(body io.ReadCloser) io.ReadCloser {
	return &bodyReader{ReadCloser: body, transfer: transfer}
}

func (body *bodyReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	grant, err := body.transfer.acquire(len(p))
	if err != nil {
		return 0, err
	}
	n, err := body.ReadCloser.Read(p[:grant.n])
	body.transfer.refund(grant, grant.n-n)
	return n, err
}

type responseWriter struct {
	http.ResponseWriter
	transfer *Transfer
}

type flushingWriter struct{ *responseWriter }

// Writer 始终逐块检查最新配置，即使请求开始时没有限速。
func (transfer *Transfer) Writer(writer http.ResponseWriter, method string) http.ResponseWriter {
	// net/http 会抑制 HEAD 的实体；错误处理器仍可能调用 Write，不能因此等待或扣额。
	if method == http.MethodHead {
		return writer
	}
	wrapped := &responseWriter{ResponseWriter: writer, transfer: transfer}
	if _, ok := writer.(http.Flusher); ok {
		return &flushingWriter{wrapped}
	}
	return wrapped
}

func (writer *responseWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }
func (writer *responseWriter) FlushError() error {
	return http.NewResponseController(writer.ResponseWriter).Flush()
}
func (writer *flushingWriter) Flush() { _ = writer.FlushError() }

func (writer *responseWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return writer.ResponseWriter.Write(p)
	}
	total := 0
	for len(p) > 0 {
		grant, err := writer.transfer.acquire(len(p))
		if err != nil {
			return total, err
		}
		n, err := writer.ResponseWriter.Write(p[:grant.n])
		writer.transfer.refund(grant, grant.n-n)
		total += n
		if err != nil {
			return total, err
		}
		if n != grant.n {
			return total, io.ErrShortWrite
		}
		p = p[n:]
	}
	return total, nil
}

// ReadFrom 不透传底层优化路径，确保 FileServer/io.Copy 也只能通过受限 Write。
func (writer *responseWriter) ReadFrom(reader io.Reader) (int64, error) {
	return io.CopyBuffer(struct{ io.Writer }{writer}, struct{ io.Reader }{reader}, make([]byte, chunkSize))
}
