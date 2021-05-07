package sparql

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/knakk/rdf"
	"github.com/knakk/sparql"
)

var CacheLocation string = ".snowman/cache/"

type Repository struct {
	Endpoint     string
	Client       *http.Client
	CacheDefault bool
	CacheHashes  map[string]bool
}

func NewRepository(endpoint string, client *http.Client, cacheDefault bool) (*Repository, error) {
	repo := Repository{
		Endpoint:     endpoint,
		Client:       http.DefaultClient,
		CacheDefault: cacheDefault,
	}

	// cache hashes are used even if caching is turned off to avoid issuing duplicate queries during build.
	repo.CacheHashes = make(map[string]bool)

	if cacheDefault {
		cacheFiles, err := ioutil.ReadDir(CacheLocation)
		if err != nil {
			return nil, err
		}

		for _, f := range cacheFiles {
			fileCacheHash := strings.Replace(f.Name(), ".json", "", 1)
			repo.CacheHashes[fileCacheHash] = true
		}
	}

	return &repo, nil
}

func (r *Repository) QueryCall(query string) (*string, error) {
	form := url.Values{}
	form.Set("query", query)
	b := form.Encode()

	req, err := http.NewRequest("POST", r.Endpoint, bytes.NewBufferString(b))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Content-Length", strconv.Itoa(len(b)))
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var responseString string
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("Received bad response from SPARQL endpoint.")
	}

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	responseString = string(bodyBytes)

	return &responseString, nil
}

func (r *Repository) Query(query string) ([]map[string]rdf.Term, error) {
	hash := sha256.Sum256([]byte(query))
	hashString := hex.EncodeToString(hash[:])
	queryCacheLocation := CacheLocation + hashString + ".json"

	// only issue queries if cache is disabled or if the query can't be found in the the cache hashes
	if !r.CacheDefault || !r.CacheHashes[hashString] {
		jsonBody, err := r.QueryCall(query)
		if err != nil {
			return nil, err
		}

		if err := os.MkdirAll(filepath.Dir(queryCacheLocation), 0770); err != nil {
			return nil, err
		}

		f, err := os.Create(queryCacheLocation)
		if err != nil {
			return nil, err
		}
		_, err = f.WriteString(*jsonBody)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		f.Sync()

		r.CacheHashes[hashString] = true
	}

	reader, err := os.Open(queryCacheLocation)
	if err != nil {
		return nil, err
	}

	parsedResponse, err := sparql.ParseJSON(reader)
	if err != nil {
		return nil, err
	}

	return parsedResponse.Solutions(), nil
}

func (r *Repository) DynamicQuery(queryLocation string, argument string) ([]map[string]rdf.Term, error) {
	fmt.Println("Issuing dynamic query " + queryLocation + " with argument " + argument)
	queryPath := "queries/" + queryLocation + ".rq"
	if _, err := os.Stat(queryPath); err != nil {
		return nil, err
	}

	sparqlBytes, err := ioutil.ReadFile(queryPath)
	if err != nil {
		return nil, err
	}

	sparqlString := strings.Replace(string(sparqlBytes), "{{.}}", argument, 1)
	parsedResponse, err := r.Query(sparqlString)
	if err != nil {
		return nil, err
	}

	return parsedResponse, nil
}
