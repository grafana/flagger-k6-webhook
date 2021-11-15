package handlers

import "net/http"

func HandleHealth(resp http.ResponseWriter, req *http.Request) {
	resp.WriteHeader(200)
	resp.Write([]byte("Good to go!")) //nolint:errcheck
}
