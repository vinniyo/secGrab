package secGrab

import (
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	currentFile  = ""
	firstListing = ""
)

//https://www.sec.gov/cgi-bin/browse-edgar?action=getcurrent&type=4&company=&dateb=&owner=include&start=0&start=5000&count=100&output=atom

func GetFeed(db *sql.DB) {
	for {
		var feedData Feed
		err := callFeed(&feedData)
		if err != nil {
			fmt.Println("here is the error getting feeddata", err)
			time.Sleep(time.Minute * 6)
			continue
		}

		tempfirstListing := ""
		re := regexp.MustCompile("http://www.sec.gov/Archives/edgar/data/[0-9]+/([0-9]+)/[0-9-]+index.htm")

		for interationCount, entry := range feedData.Entry {

			if firstListing == entry.Link.Href {
				fmt.Println("We found an entry we used before at count:", interationCount, "going back to loop")
				break
			}
			if interationCount == 1 {
				fmt.Println("#########################")
				fmt.Println("This is the first entry, setting first listing: ", entry.Link.Href)
				//adds first listing but waits till end.
				tempfirstListing = entry.Link.Href
			}

			id := re.FindStringSubmatch(entry.Link.Href)
			//fmt.Println("HERE IS THE ID: ", id)
			if len(id) == 2 {
				var date string
				errCheckDate := db.QueryRow("SELECT date FROM Filing WHERE ID = ? AND Sold = '0'", id[1]).Scan(&date)
				if errCheckDate != nil {

					//regexp check to skip duplicates
					re, _ := regexp.Compile("/([0-9-]+)-index.htm")
					match := re.FindStringSubmatch(entry.Link.Href)
					if len(match) != 2 {
						continue
					} else if match[1] == currentFile {
						//fmt.Println("skip the duplicate because there are two.")
						continue
					} else {
						currentFile = match[1]
					}
					//rootUrl := regexp.MustCompile("/[0-9-]+index.htm").ReplaceAllString(entry.Link.Href, "/")
					//fmt.Println("HERE IS ROOT URL: ", rootUrl)
					//fmt.Println("TRYING THIS: ", id[0])
					xmlUrl, errXML := getXmlUrl(&id[0])
					if errXML != nil {
						fmt.Println("Error getting XML URL", errXML)
						continue
					}
					//txtFile := strings.Replace(entry.Link.Href, "-index.htm", ".txt", 1)
					errGrab := GrabXml(&xmlUrl, &id[1], db)
					if errGrab != nil {
						//shows txt files that do not have the <XML>
						fmt.Println(errGrab)
					}
				}
			}
		}
		firstListing = tempfirstListing
		time.Sleep(time.Minute * 6)
	}
}

func callFeed(feedData *Feed) error {

	url := "https://www.sec.gov/cgi-bin/browse-edgar?action=getcurrent&type=4&company=&dateb=&owner=include&start=1&count=100&output=atom"
	resp, err := http.Get(url)

	if err != nil {
		fmt.Println("Get sec main xml error: %s", err)
		return errors.New("")
	}
	defer resp.Body.Close()

	//Golang XML package has a hard time with this encoding. So we remove the encoding type...Dunno
	content, _ := ioutil.ReadAll(resp.Body)
	content2 := strings.Replace(string(content), `encoding="ISO-8859-1"`, "", 1)
	content = []byte(content2)

	xml.Unmarshal(content, &feedData)
	fmt.Println("title of feed: ", feedData.Title, "found: ", len(feedData.Entry))
	return nil
}

//This calls the root of the filing to get the XML url. Eg: https://www.sec.gov/Archives/edgar/data/1532233/000138713116005309/
func getXmlUrl(rootUrl *string) (string, error) {

	resp, err := http.Get(*rootUrl)
	if err != nil {
		fmt.Println("Get sec main xml error:", err)
		return "", errors.New("Get Sec xml URL error")
	}
	defer resp.Body.Close()

	content, _ := ioutil.ReadAll(resp.Body)
	rootXmlEnd := regexp.MustCompile("href=\"(.*?.xml)\">.*?.xml").FindSubmatch(content)
	//fmt.Println("HERE IS THE ROOTXMLEND WHICH SEEMS TO BE BREAKING", rootXmlEnd)
	if len(rootXmlEnd) == 0 {
		fmt.Println("missing xml from root url:", *rootUrl)
		return "", errors.New("error finding xml URL")

	}
	rootXml := "http://www.sec.gov" + string(rootXmlEnd[1])

	return rootXml, nil

}

///////structs for https://www.sec.gov/cgi-bin/browse-edgar?action=getcurrent&type=4&company=&dateb=&owner=include&start=0&count=100&output=atom response
////////////////////////////
/////////////////////////////

type entry struct {
	XMLName xml.Name `xml:"entry"`
	Title   string   `xml:"title"`
	Link    link     `xml:"link"`
	//Summary  string   `xml:"summary"`
	//Updated  string   `xml:"updated"`
	//Catagory string   `xml:"catagory"`
	//ID       string   `xml:"id"`
}

type link struct {
	Href string `xml:"href,attr"`
}

type Feed struct {
	XMLName xml.Name `xml:"feed"`
	Title   string   `xml:"title"`
	Entry   []entry  `xml:"entry"`
}
