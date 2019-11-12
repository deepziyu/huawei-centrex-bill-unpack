package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
)

const bufSize = 118

type Reader struct {
	FilePath   string
	LineNumber int64
	hd         *Handle
}

type LineData struct {
	Fee                uint64
	IsPay, IsCheck     bool
	CType, BType       int
	FPhone, TPhone     string
	BeginTime, EndTime string
	Duration           uint32
	TPhoneType         int
}

type Handle interface {
	handle(data LineData, r *Reader)
}

func New(filePath string, handle Handle, lineNumber int64) *Reader {
	r := new(Reader)
	r.FilePath = filePath
	r.hd = &handle
	r.LineNumber = lineNumber
	return r
}

func (r *Reader) ReadAndHandle() error {
	return r.readBlock(func(line []byte) {
		data := getLineData(line)
		(*r.hd).handle(data, r)
	})
}

func (r *Reader) readBlock(hookfn func([]byte)) error {
	f, err := os.Open(r.FilePath)
	if err != nil {
		return err
	}
	defer f.Close()
	f.Seek(r.LineNumber*bufSize, 0)
	buf := make([]byte, bufSize) //一次读取多少个字节
	bfRd := bufio.NewReaderSize(f, 4096/2*bufSize)
	for {
		n, err := bfRd.Read(buf)
		r.LineNumber++
		if err != nil { //遇到任何错误立即返回，并忽略 EOF 错误信息
			if err == io.EOF {
				r.LineNumber-- //末位不理它
				return nil
			}
			return err
		}
		//println("n=",n)
		if n != bufSize {
			return errors.New("line is not 118 byte，n=" + strconv.Itoa(n))
		}
		hookfn(buf[:n]) // n 是成功读取字节数
		//return nil
	}
}

func getLineData(line []byte) LineData {
	var data LineData
	data.Fee = getFee(line)
	data.IsPay = getIsNeededPay(line)
	data.CType, data.BType = getCallType(line)
	data.TPhoneType = getToPhoneType(line)

	data.FPhone, data.TPhone = getPhone(line)
	data.IsCheck = checkCodeSum(line)
	data.BeginTime, data.EndTime = getCallTime(line)
	data.Duration = getCallDuration(line)

	/*	println("时间", beginTime, endTime, duration)
		println("电话", fPhone, tPhone)
		println("呼叫类型", cType, bType)
		println("是否计费", isPay)
		println("费用(分)", fee)
		println("字节校验", check)*/
	return data
}

func checkCodeSum(line []byte) bool {
	var sum uint8
	for _, value := range line[6:118] {
		sum += value
	}
	if sum == line[5] {
		return true
	} else {
		return false
	}
}

func getCallTime(line []byte) (string, string) {
	beginTime := parseTime(line[8:14])
	endTime := parseTime(line[14:20])
	return beginTime, endTime
}

func parseTime(cut []byte) string {
	beginTime := "20"
	beginTime += parseInt(cut[0]) + "-"
	beginTime += parseInt(cut[1]) + "-"
	beginTime += parseInt(cut[2]) + " "
	beginTime += parseInt(cut[3]) + ":"
	beginTime += parseInt(cut[4]) + ":"
	beginTime += parseInt(cut[5])
	return beginTime
}

func getCallDuration(line []byte) uint32 {
	cut := line[20:24]
	bytesBuffer := bytes.NewBuffer(cut)
	var tmp uint32
	binary.Read(bytesBuffer, binary.LittleEndian, &tmp)
	return tmp
}

func parseInt(c byte) string {
	x := uint8(c)
	y := fmt.Sprintf("%02d", x)
	return y //strconv.Itoa(int(x))
}

func getPhone(line []byte) (string, string) {
	fPhone := parsePhone(line[26:36])
	tPhone := parsePhone(line[38:48])
	return fPhone, tPhone
}
func getToPhoneType(line []byte) int {
	return int(uint8(line[37]))
}

func parsePhone(cut []byte) string {
	var phone string
	for _, value := range cut {
		temp, end := bcd2String(value)
		phone += temp
		if end {
			break
		}
	}
	return phone
}

func getIsNeededPay(line []byte) bool {
	cc := line[6]
	if cc&0x02 == 0x02 {
		//1：计费
		return true
	} else {
		//免费
		return false
	}
}

func getFee(line []byte) uint64 {
	cut := line[85:89]
	y, _ := binary.Uvarint(cut)
	//println(y)
	return y
}

func getCallType(line []byte) (int, int) {
	cc := line[67]
	temp := cc >> 4
	callType := int(int8(temp))
	temp = cc & 0x0f
	bussType := int(int8(temp))
	return callType, bussType
}

func bcd2String(bb byte) (string, bool) {
	fl := false
	var str string
	temp := bb
	temp = temp >> 4
	if temp != 0x0f {
		str += strconv.Itoa(int(int8(temp)))
	} else {
		fl = true
	}
	temp = bb
	temp = temp & 0x0f
	if temp != 0x0f {
		str += strconv.Itoa(int(int8(temp)))
	} else {
		fl = true
	}
	return str, fl
}
