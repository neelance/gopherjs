// +build js

package http

import (
	"errors"
	"io"
	"io/ioutil"
	"strconv"

	"github.com/gopherjs/gopherjs/js"
)

// streamReader implements an io.ReadCloser wrapper for ReadableStream of https://fetch.spec.whatwg.org/.
type streamReader struct {
	pending []byte
	stream  *js.Object
}

func (r *streamReader) Read(p []byte) (n int, err error) {
	if len(r.pending) == 0 {
		var (
			bCh   = make(chan []byte)
			errCh = make(chan error)
		)
		r.stream.Call("read").Call("then",
			func(result *js.Object) {
				if result.Get("done").Bool() {
					errCh <- io.EOF
					return
				}
				bCh <- result.Get("value").Interface().([]byte)
			},
			func(reason *js.Object) {
				// Assumes it's a DOMException.
				errCh <- errors.New(reason.Get("message").String())
			},
		)
		select {
		case b := <-bCh:
			r.pending = b
		case err := <-errCh:
			return 0, err
		}
	}
	n = copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *streamReader) Close() error {
	// TOOD: Cannot do this because it's a blocking call, and Close() is often called
	//       via `defer resp.Body.Close()`, but GopherJS currently has an issue with supporting that.
	//       See https://github.com/gopherjs/gopherjs/issues/381 and https://github.com/gopherjs/gopherjs/issues/426.
	/*ch := make(chan error)
	r.stream.Call("cancel").Call("then",
		func(result *js.Object) {
			if result != js.Undefined {
				ch <- errors.New(result.String()) // TODO: Verify this works, it probably doesn't and should be rewritten as result.Get("message").String() or something.
				return
			}
			ch <- nil
		},
	)
	return <-ch*/
	r.stream.Call("cancel")
	return nil
}

// fetchTransport is a RoundTripper that is implemented using Fetch API. It supports streaming
// response bodies.
type fetchTransport struct{}

func (t *fetchTransport) RoundTrip(req *Request) (*Response, error) {
	headers := js.Global.Get("Headers").New()
	for key, values := range req.Header {
		for _, value := range values {
			headers.Call("append", key, value)
		}
	}
	opt := map[string]interface{}{
		"method":  req.Method,
		"headers": headers,
		//"redirect": "manual", // Can't use this because it results in an opaque-redirect filtered response, which appears to be unfit for the purpose of completing the redirect.
	}
	if req.Body != nil {
		// TODO: Find out if request body can be streamed into the fetch request rather than in advance here.
		//       See BufferSource at https://fetch.spec.whatwg.org/#body-mixin.
		body, err := ioutil.ReadAll(req.Body)
		if err != nil {
			req.Body.Close() // RoundTrip must always close the body, including on errors.
			return nil, err
		}
		req.Body.Close()
		opt["body"] = body
	}
	respPromise := js.Global.Call("fetch", req.URL.String(), opt)

	var (
		respCh = make(chan *Response)
		errCh  = make(chan error)
	)
	respPromise.Call("then",
		func(result *js.Object) {
			header := Header{}
			result.Get("headers").Call("forEach", func(value, key *js.Object) {
				ck := CanonicalHeaderKey(key.String())
				header[ck] = append(header[ck], value.String())
			})

			// TODO: With streaming responses, this cannot be set.
			//       But it doesn't seem to be set even for non-streaming responses. In other words,
			//       this code is currently completely unexercised/untested. Need to test it. Probably
			//       by writing a http.Handler that explicitly sets Content-Type header? Figure this out.
			contentLength := int64(-1)
			if cl, err := strconv.ParseInt(header.Get("Content-Length"), 10, 64); err == nil {
				contentLength = cl
			}

			// TODO: Sort this out.
			/*var body io.ReadCloser
			if b := result.Get("body"); b != nil {
				body = &streamReader{stream: b.Call("getReader")}
			} else {
				body = noBody
			}*/

			respCh <- &Response{
				Status:        result.Get("status").String() + " " + StatusText(result.Get("status").Int()),
				StatusCode:    result.Get("status").Int(),
				Header:        header,
				ContentLength: contentLength,
				Body:          &streamReader{stream: result.Get("body").Call("getReader")},
				Request:       req,
			}
		},
		func(reason *js.Object) {
			errCh <- errors.New("net/http: fetch() failed")
		},
	)
	select {
	case resp := <-respCh:
		return resp, nil
	case err := <-errCh:
		return nil, err
	case <-req.Cancel:
		// TODO: Abort request if possible using Fetch API.
		return nil, errors.New("net/http: request canceled")
	}
}

// TODO: Consider implementing here if importing those 2 packages is expensive.
//var noBody io.ReadCloser = ioutil.NopCloser(bytes.NewReader(nil))
