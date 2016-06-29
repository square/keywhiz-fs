package main

import (
    "fmt"
    "log"
    "net/http"
    "encoding/json"
    "encoding/base64"
    "sync/atomic"

    "github.com/gorilla/mux"
)

type Secret struct {
    Name          string  `json:"name"`
    Secret        string  `json:"secret"`
    SecretLength  int     `json:"secretLength"`
    CreationDate  string  `json:"creationDate"`
}

var counter uint32

func main() {
    router := mux.NewRouter()
    router.HandleFunc("/secret/{secretName}", GetSecret)
    router.HandleFunc("/secrets", ListSecrets)


    err := http.ListenAndServeTLS(":8080", "server.crt", "server.key", router)
    if err != nil {
      log.Fatal("ListenAndServe: ", err)
    }
}

func ListSecrets(w http.ResponseWriter, r *http.Request) {
  secrets := []Secret{
    Secret{Name:"test_secret", Secret:"", SecretLength:0, CreationDate:"2016-06-29T20:05:21.000Z"},
  }
  json.NewEncoder(w).Encode(secrets)
}

func GetSecret(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    secretName := vars["secretName"]
    if secretName == "test_secret" {
      c := atomic.AddUint32(&counter, 1)
      s := fmt.Sprintf("hello_%d", c)
      secret := Secret{Name: "test_secret", Secret: base64.StdEncoding.EncodeToString([]byte(s)),
        SecretLength: 12, CreationDate: "2016-06-29T20:05:21.000Z"}
      json.NewEncoder(w).Encode(secret)
    } else {
      w.WriteHeader(http.StatusNotFound)
      fmt.Fprintln(w, "<html><body>HTTP ERROR 404</body></html>")
    }
}
