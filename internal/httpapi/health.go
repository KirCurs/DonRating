package httpapi

import (
	"fmt"
	"net/http"
)

func Health(w http.ResponseWriter, r *http.Request) { // доступность сервера
	fmt.Printf("Метод: %s\n", r.Method)
	fmt.Fprintln(w, "ok")

}
