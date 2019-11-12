package main

import (
	"github.com/sirupsen/logrus"
	"os"
	"time"
)

type FileLogHook struct {
	Writer *os.File
}

func NewFileHook() *FileLogHook {
	var file *os.File
	var err error
	now := time.Now()

	logFilePath := getCurrentPath() + logDir + now.Format(date) + ".log"
	if !checkFileIsExist(logFilePath) {
		err = os.MkdirAll(getCurrentPath()+logDir, 0777)
		file, err = os.Create(logFilePath)
		if err != nil {
			panic(err)
		}
	} else {
		file, err = os.OpenFile(logFilePath, os.O_APPEND, 0666)
		if err != nil {
			panic(err)
		}
	}
	return &FileLogHook{file}
}


func (hook *FileLogHook) Fire(entry *logrus.Entry) error {
	line, _ := entry.String()
	_,err := hook.Writer.WriteString(line)
	return  err
}

func (hook *FileLogHook) Levels() []logrus.Level {
	return logrus.AllLevels
}