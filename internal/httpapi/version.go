package httpapi

import (
	"fmt"
	"net/http"
)

func Version(w http.ResponseWriter, r *http.Request) { // версия сервиса
	fmt.Fprintln(w, "0.1.0")
	fmt.Printf("Метод %s\n", r.Method)
}
