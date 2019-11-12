package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	logDir         = "log\\"
	pointFile      = "point.save"
)

const (
	date     = "2006-01-02"
	date2    = "20060102"
	//datetime = "2006-01-02 15:04:05"
)

type MyHandle struct {
	bilDate     string
	bilDateOb   time.Time
	lineNumber  int64
	outConfig   *OutConfig
	Chan        chan int
	reserveList sync.Map //[string]ReserveData
}

type ReserveData struct {
	Data         LineData
	ReserveTimes int
	ReserveAt    time.Time
}

type OutConfig struct {
	BilPath    string        `yaml:"bilPath"`
	UploadUrl  string        `yaml:"uploadUrl"`
	UploadCons int           `yaml:"uploadCons"`
	MaxRetry   int           `yaml:"maxRetry"`
	TimeOut    time.Duration `yaml:"timeOut"`
}

var (
	configFile  = flag.String("configFile", "config.yaml", "General configuration file")
	reserveFile = flag.String("reserveFile", "reserveList.save", "map-json for reserve next time")
)

var logger = logrus.New()

func main() {
	hook := NewFileHook()
	logger.AddHook(hook)

	/*aChan := make(chan int, 1)
	timer1 := time.NewTicker(10 * time.Second)
	aChan <- 1
	for {
		select {
		case <-aChan:
			mainTimer()
			break
		case <-timer1.C:
			if len(aChan) < 1 {
				aChan <- 1
			}
			break
		}
	}*/

	mainTimer()
}

func mainTimer() {
	handle := getNowHandle()

	logger.Info("begin1:", handle.bilDate, handle.lineNumber)
	handle.handleReserve()

	logger.Info("begin2:", handle.bilDate, handle.lineNumber)
	handle.run()

	handle.toNextDay()
	logger.Info("begin3:", handle.bilDate, handle.lineNumber)
	handle.run()

	handle.close()
}

func getNowHandle() *MyHandle {
	path := getCurrentPath() + pointFile
	buf, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	str := strings.SplitN(string(buf), "|", 2)
	handle := new(MyHandle)
	handle.parseConf()
	handle.Chan = make(chan int, handle.outConfig.UploadCons+1)
	handle.Chan <- 1 //先占用一个，用来等大雨停 天空明 春花开 秋叶落，用来等岁月长 时间老 容颜易 佳酿醇
	handle.bilDate = str[0]
	handle.lineNumber, _ = strconv.ParseInt(str[1], 10, 64)
	handle.bilDateOb, _ = time.Parse(date2, handle.bilDate)
	handle.parseReserve()
	//fmt.Printf("%v", handle)
	return handle
}

func (m *MyHandle) parseConf() {
	configPath := getCurrentPath() + *configFile
	buf, err := ioutil.ReadFile(configPath)
	if err != nil {
		logger.Fatal("Fail to open:"+configPath, err.Error())
	}
	c := &OutConfig{}
	if err := yaml.Unmarshal(buf,c); err!=nil{
		logger.Fatal("Fail to open:"+configPath, err.Error())
	}
	m.outConfig = c
}

func (m *MyHandle) run() {
	read := New(m.getNowPath(), m, m.lineNumber)
	if err := read.ReadAndHandle(); err!= nil{
		logger.Fatal("Fail to run:" + read.FilePath + (string(m.lineNumber)))
	}
	m.savePoint(read)
}
func (m *MyHandle) close() {
	maxTimes := m.outConfig.UploadCons * int(m.outConfig.TimeOut)
	times := 0
	for {
		<-m.Chan //隐约雷鸣 阴霾天空 但盼风雨来 能留你在此
		if len(m.Chan) == 0 || times > maxTimes {
			break
		}
		m.Chan <- 1
		time.Sleep(time.Duration(1) * time.Second)
		times++
	}
	close(m.Chan)
	m.saveReserve()
	logger.Info("close:", m.bilDate, m.lineNumber)
}

func (m *MyHandle) toNextDay() {
	nowTime := time.Now()
	if nowTime.Format(date) > m.bilDateOb.Format(date) {
		m.bilDateOb = m.bilDateOb.Add(time.Hour * 24)
		m.bilDate = m.bilDateOb.Format(date2)
		m.lineNumber = 0
	}
}

func (m *MyHandle) handle(data LineData, r *Reader) {
	//println("line: ", m.lineNumber, r.LineNumber)
	rData := ReserveData{Data: data, ReserveTimes: 0, ReserveAt: time.Now()}
	m.savePoint(r)
	m.logData(data, r)
	m.Chan <- 1
	go func() {
		_ = m.uploadData(rData)
		<-m.Chan
	}()
}

func (m *MyHandle) getNowPath() string {
	return m.outConfig.BilPath + m.bilDate + ".bil"
}

func (m *MyHandle) savePoint(r *Reader) {
	var data []byte
	m.lineNumber = r.LineNumber
	path := getCurrentPath() + pointFile
	data = append([]byte(m.bilDate + "|" + strconv.FormatInt(m.lineNumber, 10)))
	_ = ioutil.WriteFile(path, data, 0777)
}

func (m *MyHandle) uploadData(data ReserveData) error {
	defer func() {
		println("exit")
		os.Exit(11)
	}()
	jsonData, err := json.Marshal(data)
	jsonDataByte := bytes.NewBuffer(jsonData)
	if err != nil {
		return err
	}

	if data.ReserveTimes > m.outConfig.MaxRetry {
		logger.Error("out data ", string(jsonData))
		return nil
	}

	reserveFun := func(t int, at time.Time) {
		md5Ob := md5.New()
		md5Ob.Write(jsonData)
		key := md5Ob.Sum(nil)
		keyS := hex.EncodeToString(key)
		data.ReserveTimes += t
		data.ReserveAt = at
		m.reserveList.Store(keyS, data)
	}

	const NG = 53 //倍数 51 秒
	now := time.Now()
	if data.ReserveTimes > 0 && int(now.Unix()-data.ReserveAt.Unix()) < data.ReserveTimes*data.ReserveTimes*NG {
		reserveFun(0, data.ReserveAt)
		return nil
	}
	timeOut := m.outConfig.TimeOut * time.Second
	resp, err := Post(m.outConfig.UploadUrl, jsonDataByte, timeOut)
	if err != nil {
		logger.Error(err.Error())
		reserveFun(1, now)
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logger.Error(err.Error())
		return err
	}
	if resp.StatusCode != 200 || strings.Contains(string(body), "500") {
		logger.Error(string(body))
		reserveFun(1, now)
		return errors.New("server error")
	}
	return err
}

func Post(url string, body io.Reader, timeout time.Duration) (resp *http.Response, err error) {
	req, err := http.NewRequest("POST", url, body)
	client := &http.Client{Timeout: timeout}
	if err != nil {
		return nil, err
	}
	salt := "vanke-frs-lizhenju"
	no := strconv.FormatInt(time.Now().Unix(), 10)
	accessToken := "php is the best lg in the world"
	sign := Hmacs(no+salt, accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("accessToken", accessToken)
	req.Header.Set("no", no)
	req.Header.Set("bug", sign)
	return client.Do(req)
}

func Hmacs(key, data string) string {
	ob := hmac.New(sha1.New, []byte(key))
	ob.Write([]byte(data))
	return hex.EncodeToString(ob.Sum([]byte("")))
}

func (m *MyHandle) saveReserve() {
	fileName := getCurrentPath() + logDir + *reserveFile
	dataS := make(map[string]ReserveData)
	m.reserveList.Range(func(key, value interface{}) bool {
		k := key.(string)
		d := value.(ReserveData)
		dataS[k] = d
		return true
	})
	jsonData, _ := json.Marshal(dataS)
	_ = ioutil.WriteFile(fileName, jsonData, 0777)
}

func (m *MyHandle) parseReserve() {
	defer func() {
		/*if m.reserveList. == nil {
			m.reserveList = make(map[string]ReserveData)
		}*/
	}()
	fileName := getCurrentPath() + logDir + *reserveFile
	content, err := ioutil.ReadFile(fileName)
	if err != nil {
		logger.Error("parseReserve.ReadFile:", err.Error())
		return
	}
	dataS := make(map[string]ReserveData)
	err = json.Unmarshal(content, &(dataS))
	if err != nil {
		logger.Error("parseReserve.Unmarshal:", err.Error())
		return
	}
	for key, value := range dataS {
		delete(dataS, key)
		m.reserveList.Store(key, value)
	}
}

func (m *MyHandle) handleReserve() {
	m.reserveList.Range(func(key, value interface{}) bool {
		data := value.(ReserveData)
		m.reserveList.Delete(key)
		m.Chan <- 1
		go func() {
			_ = m.uploadData(data)
			<-m.Chan
		}()
		return true
	})
}

func (m *MyHandle) logData(data LineData, r *Reader) {
	logger.Info(data)
}

func getCurrentPath() string {
	s, err := exec.LookPath(os.Args[0])
	if err != nil {
		panic(err)
	}
	i := strings.LastIndex(s, "\\")
	path := s[0 : i+1]
	return path
}

/**
 * 判断文件是否存在  存在返回 true 不存在返回false
 */
func checkFileIsExist(filename string) bool {
	var exist = true
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		exist = false
	}
	return exist
}
