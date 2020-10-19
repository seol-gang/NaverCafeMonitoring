package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/tebeka/selenium"
	"github.com/tebeka/selenium/chrome"
)

type Author struct {
	ID string `json:"id"`
	PW string `json:"pw"`
}

type ArticleInfo struct {
	noticeType     string
	noticeIDNum    int64
	noticeTitle    string
	noticeLink     string
	noticeFilePath string
	nickname       string
	id             string
	dateTime       string
}

func Enabled(by, elementName string) func(selenium.WebDriver) (bool, error) {
	return func(wd selenium.WebDriver) (bool, error) {
		el, err := wd.FindElement(by, elementName)
		if err != nil {
			return false, nil
		}
		enabled, err := el.IsEnabled()
		if err != nil {
			return false, nil
		}

		if !enabled {
			return false, nil
		}

		return true, nil
	}
}

func StartSelenium(wd *selenium.WebDriver, port int) {
	caps := selenium.Capabilities{"browserName": "chrome"}
	chromeCaps := chrome.Capabilities{
		Path: "",
	}
	caps.AddChrome(chromeCaps)

	_, err := selenium.NewChromeDriverService("chromedriver.exe", 4444)
	//defer service.Stop()

	*wd, err = selenium.NewRemote(caps, "")
	if err != nil {
		fmt.Println(err)
	}
	//defer wd.Quit()
	LoginNaver(*wd)
}

func LoginNaver(wd selenium.WebDriver) {
	wd.Get("https://nid.naver.com/nidlogin.login")

	if err := wd.Wait(Enabled(selenium.ByCSSSelector, "#log\\.login")); err != nil {
		fmt.Print(err)
	}

	b, err := ioutil.ReadFile("./setting.json")
	if err != nil {
		fmt.Println(err)
	}
	var account Author
	json.Unmarshal(b, &account)

	loginScript := `
	(function execute(){
		document.querySelector('#id').value = '` + account.ID + `';
		document.querySelector('#pw').value = '` + account.PW + `';
	})();`

	wd.ExecuteScript(loginScript, nil)

	btn, err := wd.FindElement(selenium.ByXPATH, "//*[@id=\"log.login\"]")
	btn.Click()

	if err := wd.Wait(Enabled(selenium.ByXPATH, "//*[@id=\"footer\"]/div/div[4]/address/a")); err != nil {
		fmt.Print(err)
	}
}

func main() {
	var wd selenium.WebDriver
	var wd2 selenium.WebDriver
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		StartSelenium(&wd, 4444)
	}()
	go func() {
		defer wait.Done()
		StartSelenium(&wd2, 4445)
	}()
	wait.Wait()

	done := make(chan bool)
	articleChan := make(chan []ArticleInfo, 50)
	dbChan := make(chan ArticleInfo, 50)
	go QueryDB(dbChan)
	go ParseArticle(wd, done, articleChan)
	go DataProcessing(wd2, articleChan, dbChan)
	<-done
}

func QueryDB(dbChan chan ArticleInfo) {
	db, err := sql.Open("mysql", "DB_ID:DB_PW@tcp(REMOTE_ADDRESS)/DB_NAME")
	if err != nil {
		fmt.Println(err)
	}
	defer db.Close()

	article := ArticleInfo{}
	for {
		article = <-dbChan
		_, err = db.Exec("INSERT INTO notice_info (notice_type, notice_number, notice_title, notice_link, notice_filepath, nickname, id, date) VALUES(?,?,?,?,?,?,?,?)",
			article.noticeType, article.noticeIDNum, article.noticeTitle, article.noticeLink,
			article.noticeFilePath, article.nickname, article.id, article.dateTime)
		if err != nil {
			fmt.Println(err)
		}
	}
}

func DataProcessing(wd selenium.WebDriver, articleChan chan []ArticleInfo, dbChan chan ArticleInfo) {
	var articleInfo []ArticleInfo
	var saveDir string = "Z:\\HDD1\\LOLKOR_ARTICLE\\"
	for {
		articleInfo = <-articleChan
		for _, data := range articleInfo {
			wd.Get(data.noticeLink)
			time.Sleep(1 * time.Second)
			if err := wd.AcceptAlert(); err == nil {
				continue
			}
			if err := wd.Wait(Enabled(selenium.ByXPATH, "//*[@id=\"cafe_main\"]")); err != nil {
				fmt.Print(err)
			}
			time.Sleep(1 * time.Second)
			if err := wd.SwitchFrame("cafe_main"); err != nil {
				fmt.Println(err)
			}

			folderName := strings.Split(data.dateTime, " ")[0]
			if _, err := os.Stat(saveDir + folderName); os.IsNotExist(err) {
				os.Mkdir(saveDir+folderName, os.ModeDir)
			}
			fileName := data.dateTime + strconv.Itoa(int(data.noticeIDNum)) + "_" + data.nickname + "_" + data.id
			fileName = strings.Replace(strings.Replace(fileName, ":", "_", -1), " ", "_", -1)
			filePath := saveDir + folderName + "\\" + fileName + ".html"
			f, err := os.Create(filePath)
			if err != nil {
				fmt.Println(err)
			}
			pageSrc, _ := wd.PageSource()
			_, err = f.WriteString(pageSrc)
			if err != nil {
				fmt.Println(err)
			}
			f.Close()

			data.noticeFilePath = filePath
			dbChan <- data
			time.Sleep(1 * time.Second)
		}
	}
}

func ParseArticle(wd selenium.WebDriver, done chan bool, articleChan chan []ArticleInfo) {
	wd.Get("NAVER CAFE ALL ARTICLE BOARD URL")

	var noticeNumChk int64 = 0
	for {
		if err := wd.SwitchFrame("cafe_main"); err != nil {
			fmt.Println(err)
		}

		d, err := wd.FindElement(selenium.ByXPATH, "//*[@id=\"main-area\"]/div[4]/table/tbody")
		if err != nil {
			fmt.Println(err)
		}
		article, err := d.FindElements(selenium.ByXPATH, "//*[@id=\"main-area\"]/div[4]/table/tbody/tr")
		if err != nil {
			fmt.Println(err)
		}

		var articleList []ArticleInfo

		for _, data := range article {
			articleInfo := ArticleInfo{}
			var wait sync.WaitGroup
			wait.Add(4)

			go func() {
				defer wait.Done()
				//게시판 목록
				li, err := data.FindElement(selenium.ByClassName, "inner_name")
				if err != nil {
					fmt.Println(err)
				}
				noticeType, err := li.Text()
				if err != nil {
					fmt.Println(err)
				}
				articleInfo.noticeType = noticeType
			}()

			go func() {
				defer wait.Done()
				//게시판 링크
				li, err := data.FindElement(selenium.ByClassName, "article")
				if err != nil {
					fmt.Println(err)
				}
				noticeLink, err := li.GetAttribute("href")
				if err != nil {
					fmt.Println(err)
				}
				//게시글 고유 번호
				r, _ := regexp.Compile("articleid=[0-9]+")
				tempID := r.FindString(noticeLink)
				noticeIDNum := strings.Split(tempID, "=")[1]
				articleInfo.noticeIDNum, _ = strconv.ParseInt(noticeIDNum, 10, 64)

				//게시글 제목
				noticeTitle, err := li.Text()
				if err != nil {
					fmt.Println(err)
				}
				articleInfo.noticeTitle = noticeTitle
				articleInfo.noticeLink = noticeLink
			}()

			go func() {
				defer wait.Done()
				//작성자 닉네임 및 아이디
				li, err := data.FindElement(selenium.ByClassName, "m-tcol-c")
				if err != nil {
					fmt.Println(err)
				}
				nickname, err := li.Text()
				if err != nil {
					fmt.Println(err)
				}
				tempIdStr, err := li.GetAttribute("onclick")
				id := strings.Trim(strings.TrimSpace(strings.Split(tempIdStr, ",")[1]), "'")
				articleInfo.nickname = nickname
				articleInfo.id = id
			}()

			go func() {
				defer wait.Done()
				//작성 시간
				li, err := data.FindElement(selenium.ByClassName, "td_date")
				if err != nil {
					fmt.Println(err)
				}
				tempTime, err := li.Text()
				if err != nil {
					fmt.Println(err)
				}
				loc, _ := time.LoadLocation("Asia/Seoul")
				dateTime := strings.Split(time.Now().In(loc).String(), " ")[0] + " " + tempTime + ":00"
				articleInfo.dateTime = dateTime
			}()
			wait.Wait()

			if noticeNumChk >= articleInfo.noticeIDNum {
				break
			}

			var filterNoticeType []string
			filterNoticeType = []string{"NAVER CAFE BOARD LIST FOR MONITORING"}
			for _, val := range filterNoticeType {
				if val == articleInfo.noticeType {
					articleList = append(articleList, articleInfo)
				}
			}
		}

		time.Sleep(3 * time.Second)

		if len(articleList) != 0 {
			noticeNumChk = articleList[0].noticeIDNum
		}
		articleChan <- articleList
		wd.Refresh()
	}

	done <- true
}
