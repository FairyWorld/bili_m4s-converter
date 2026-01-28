package conver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/mzky/converter"
)

func Xml2Ass(xml string) string {
	dstFile := ""
	xmlState, err := os.Stat(xml)
	if err != nil {
		if os.IsNotExist(err) {
			logrus.Warnf("文件：%s不存在", xml)
			return dstFile
		}
		logrus.Warn(err)
		return dstFile
	}

	xmls, err := listXmlFiles(xml, xmlState)
	if err != nil {
		logrus.Warnf("无法列出XML文件：%v", err)
		return dstFile
	}

	setting := DefaultSetting
	assConfig := setting.GetAssConfig()
	chain := converter.NewFilterChain()
	keywordFilter, typeFilter := setting.GetFilter()
	chain.AddFilter(keywordFilter).AddFilter(typeFilter)

	failed := 0
	for _, file := range xmls {
		// 加载xml文件
		src, err := os.Open(file)
		if err != nil {
			logrus.Warnf("无法打开XML文件：%v", err)
			failed++
			continue
		}

		dstFile = strings.ReplaceAll(file, filepath.Ext(file), AssSuffix)
		dst, e := os.Create(dstFile)
		if e != nil {
			logrus.Warnf("无法创建ASS文件：%v", e)
			failed++
			_ = src.Close()
			continue
		}

		// 添加panic恢复机制，防止XML文件格式错误导致软件崩溃
		func() {
			defer func() {
				if r := recover(); r != nil {
					logrus.Warnf("处理XML文件时发生错误：%v，跳过生成字幕", r)
					failed++
				}
				_ = src.Close()
				_ = dst.Close()
			}()

			// 如果在go程中加载xml，当文件过多时会出现过高的内存占用
			pool := converter.LoadPool(src, chain)
			if er := pool.Convert(dst, assConfig); er != nil {
				logrus.Warnf("转换XML到ASS失败：%v", er)
				failed++
			}
		}()
	}
	// fmt.Println("转换弹幕:", "成功数", len(xmls)-failed, "失败数", failed)
	return dstFile
}

func listXmlFiles(xml string, xmlState os.FileInfo) ([]string, error) {
	if xmlState.IsDir() {
		if xml[len(xml)-1] != os.PathSeparator {
			xml += string(os.PathSeparator)
		}
		entries, err := os.ReadDir(xml)
		if err != nil {
			return nil, fmt.Errorf("无法读取目录：%v", err)
		}
		var xmls []string
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), XmlSuffix) {
				xmls = append(xmls, filepath.Join(xml, entry.Name()))
			}
		}
		return xmls, nil
	} else if strings.HasSuffix(xml, XmlSuffix) {
		return []string{xml}, nil
	}
	return nil, fmt.Errorf("不支持的文件格式")
}
