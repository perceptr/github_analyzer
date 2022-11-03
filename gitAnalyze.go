package main

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/sync/semaphore"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var token = "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"
var ctx = context.Background()
var sleepTime = 100 * time.Millisecond
var semNumber = 0
var userChoice = "safe"
var sem = semaphore.NewWeighted(int64(semNumber))

func main() {
	var orgName = "kontur-edu"
	fmt.Println("Enter 'safe' if you want to safely analyze the organization " +
		"or 'fast' if you want to analyze it faster")

	_, err := fmt.Scan(&userChoice)
	if err != nil {
		return
	}
	if userChoice == "fast" {
		semNumber = 220
		sleepTime = 60 * time.Millisecond
		sem = semaphore.NewWeighted(int64(semNumber))
	} else {
		semNumber = 10
		sleepTime = 100 * time.Millisecond
		sem = semaphore.NewWeighted(int64(semNumber))
	}

	getOrgTopUsers(orgName)
}

func getOrgTopUsers(orgName string) {
	defer timeTrack(time.Now(), "getOrgTopUsers")
	var url = "https://api.github.com/orgs/" + orgName
	var urlForRepos = url + "/repos"
	var allRepos = getAllOrgRepos(urlForRepos)
	var statistics = map[string]int{}

	var waitGroup sync.WaitGroup
	for _, repo := range allRepos {
		var commitUrl = "https://api.github.com/repos/" + orgName + "/" + repo + "/commits"
		waitGroup.Add(1)

		go func() {
			var emails = getAllEmailsInRepo(commitUrl)
			for _, email := range emails {
				statistics[email]++
			}
			waitGroup.Done()
		}()
	}
	waitGroup.Wait()

	var sortedKeys = sortMapByValue(statistics)
	for count, email := range sortedKeys {
		fmt.Printf("%d) Email: %s, Количество комитов: %d\n", count+1, email, statistics[email])
		if count == 99 {
			break
		}
	}
}

func sortMapByValue(statistics map[string]int) []string {
	keys := make([]string, 0, len(statistics))

	for key := range statistics {
		keys = append(keys, key)
	}

	sort.SliceStable(keys, func(i, j int) bool {
		return statistics[keys[i]] > statistics[keys[j]]
	})
	return keys
}

func getAllReposInPage(url string) []string {
	var resp = makeHTTPGetRequest(url)
	var data []interface{}
	body, err := io.ReadAll(resp.Body)

	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(body, &data)
	if err != nil {
		panic(err)
	}

	var allRepos []string
	for _, repo := range data {
		var repoMap = repo.(map[string]interface{})
		allRepos = append(allRepos, repoMap["name"].(string))
	}
	return allRepos
}

func getAllOrgRepos(url string) []string {
	var allItems []string
	var waitGroup sync.WaitGroup
	var number = getAllReposInOrgNumber(strings.Split(url, "/")[4])/100 + 1

	for i := 0; i < number; i++ {
		waitGroup.Add(1)

		go func(i int) {
			var pageUrl = url + "?per_page=100&page=" + strconv.Itoa(i+1)
			var items = getAllReposInPage(pageUrl)
			allItems = append(allItems, items...)
			waitGroup.Done()
		}(i)
	}
	waitGroup.Wait()

	return allItems
}

func getAllEmailsInRepo(commitUrl string) []string {
	var number = getRepoCommitsNumber(commitUrl)/100 + 1
	var allEmails []string
	var waitGroup sync.WaitGroup

	for i := 0; i < number; i++ {
		waitGroup.Add(1)

		go func(i int) {
			var pageUrl = commitUrl + "?per_page=100&page=" + strconv.Itoa(i+1)
			var emails = getAllEmailsInGivenPage(pageUrl)
			if emails != nil {
				allEmails = append(allEmails, emails...)
			}
			waitGroup.Done()
		}(i)
	}
	waitGroup.Wait()

	return allEmails
}

func getAllEmailsInGivenPage(url string) []string {
	var resp = makeHTTPGetRequest(url)
	var data []interface{}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	if strings.Contains(string(body), "API rate limit exceeded") {
		fmt.Println("API rate limit exceeded")
	}
	if strings.Contains(string(body), "Git Repository is empty") {
		return nil
	}

	err = json.Unmarshal(body, &data)
	if err != nil {
		fmt.Println(string(body))
		panic(err)
	}

	var allEmails []string
	for _, commit := range data {
		var commitMap = commit.(map[string]interface{})
		var author = commitMap["commit"].(map[string]interface{})["author"].(map[string]interface{})
		var message = commitMap["commit"].(map[string]interface{})["message"].(string)

		if strings.Contains(message, "Merge pull request #") || author["email"] == "" {
			continue
		}

		allEmails = append(allEmails, author["email"].(string))
	}
	return allEmails
}

func makeHTTPGetRequest(url string) *http.Response {
	if sem.Acquire(ctx, 1) != nil {
		panic("semaphore acquire error")
	}
	var client = &http.Client{}
	req, err := http.NewRequest("GET", url, nil)

	if err != nil {
		fmt.Println(err)
	}
	req.Header.Add("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
	}

	time.Sleep(sleepTime)
	sem.Release(1)

	return resp
}

func getRepoCommitsNumber(commitUrl string) int {
	var urlInp = commitUrl + "?per_page=1"
	var resp = makeHTTPGetRequest(urlInp)

	links := resp.Header["Link"]
	if len(links) == 0 {
		return 0
	}
	var badlyParsedPageNumber = strings.Split(strings.Split(strings.Split(links[0], ", ")[1], "; ")[0],
		"&page=")[1]
	lastPage := badlyParsedPageNumber[0 : len(badlyParsedPageNumber)-1]

	var number, err1 = strconv.Atoi(lastPage)
	if err1 != nil {
		fmt.Println(err1)
	}
	return number
}

func getAllReposInOrgNumber(org string) int {
	var url = "https://api.github.com/orgs/" + org
	var resp = makeHTTPGetRequest(url)
	var data map[string]interface{}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(body, &data)
	if err != nil {
		panic(err)
	}
	var number = int(data["public_repos"].(float64))

	return number
}

func timeTrack(start time.Time, name string) {
	elapsed := time.Since(start).Seconds()
	log.Printf("%s took %f seconds", name, elapsed)
}
