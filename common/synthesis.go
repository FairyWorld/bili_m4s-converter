package common

import (
	"fmt"
	"m4s-converter/conver"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bitly/go-simplejson"
	"github.com/fatih/color"
	utils "github.com/mzky/utils/common"
	"github.com/sirupsen/logrus"
)

func (c *Config) Synthesis() {
	begin := time.Now().Unix()
	logrus.Println("查找缓存目录下可转换的文件...")
	// 查找m4s文件，并转换为mp4和mp3
	if err := filepath.WalkDir(c.CachePath, c.FindM4sFiles); err != nil {
		MessageBox(fmt.Sprintf("查找并转换 m4s 文件异常：%v", err))
		c.wait()
	}

	dirs, err := GetCacheDir(c.CachePath) // 缓存根目录模式
	if err != nil {
		MessageBox(fmt.Sprintf("找不到 BiliBili 的缓存目录：%v", err))
		c.wait()
	}

	if dirs == nil {
		// 判断非缓存根目录时，验证是否为子目录
		if utils.IsExist(filepath.Join(c.CachePath, conver.VideoInfoSuffix)) ||
			utils.IsExist(filepath.Join(c.CachePath, conver.VideoInfoJson)) {
			dirs = append(dirs, c.CachePath)
		}
	}

	// 合成音视频文件
	c.OutputDir = filepath.Join(c.CachePath, "output")
	var outputFiles []string
	var skipFilePaths []string
	for _, v := range dirs {
		video, audio, e := c.GetAudioAndVideo(v)
		if e != nil {
			logrus.Error("找不到已修复的音频和视频文件:", e)
			continue
		}
		info := filepath.Join(v, conver.VideoInfoJson)
		if !utils.IsExist(info) {
			info = filepath.Join(v, conver.VideoInfoSuffix)
			if !utils.IsExist(info) {
				info = filepath.Join(v, conver.PlayEntryJson)
				if !utils.IsExist(info) {
					continue
				}
			}
		}
		infoStr, e := os.ReadFile(info)
		if e != nil {
			logrus.Error("找不到包含视频信息的info相关文件: ", info)
			continue
		}
		js, e := simplejson.NewJson(infoStr)
		if e != nil {
			logrus.Error("videoInfo相关文件解析失败: ", info)
			continue
		}

		groupTitle := Filter(js.Get("groupTitle").String())
		groupTitle = null2Str(groupTitle, Filter(js.Get("owner_name").String()))

		title := Filter(js.Get("page_data").Get("download_subtitle").String())
		title = null2Str(title, Filter(js.Get("title").String()))

		uname := Filter(js.Get("uname").String())
		uname = null2Str(uname, Filter(js.Get("title").String()))

		status := Filter(js.Get("status").String())
		status = null2Str(status, Filter(js.Get("page_data").Get("download_title").String()))

		itemId, e := js.Get("itemId").Int()
		if itemId == 0 || e != nil {
			itemId, _ = js.Get("owner_id").Int()
		}
		c.ItemId = strconv.Itoa(itemId)

		if status != "completed" && status != "视频已缓存完成" && status != "" {
			skipFilePaths = append(skipFilePaths, v)
			logrus.Warn("未缓存完成,跳过合成", v, title+"-"+uname)
			continue
		}
		if !utils.IsExist(c.OutputDir) {
			_ = os.MkdirAll(c.OutputDir, os.ModePerm)
		}
		groupPath := groupTitle + "-" + uname
		groupDir := filepath.Join(c.OutputDir, groupPath)
		if !utils.IsExist(groupDir) {
			if err = os.MkdirAll(groupDir, os.ModePerm); err != nil {
				MessageBox("无法创建目录：" + groupDir)
				c.wait()
			}
		}
		mp4Name := title + conver.Mp4Suffix
		outputFile := filepath.Join(groupDir, mp4Name)

		// 检查目录中是否存在与输入音频和视频文件内容相同的文件
		if exists, existingFile := c.isIdenticalFileExists(groupDir, video, audio); exists {
			logrus.Warn("跳过完全相同的视频: ", existingFile)
			continue
		}

		// 检查文件是否存在，如果存在且不覆盖，则重命名
		if utils.IsExist(outputFile) && !c.Overlay {
			mp4Name = title + "-" + c.ItemId + conver.Mp4Suffix
			outputFile = filepath.Join(groupDir, mp4Name)
		} else if utils.IsExist(outputFile) && c.Overlay {
			// 如果设置了覆盖，则直接使用原始文件名，让MP4Box或FFmpeg覆盖已存在的文件
			logrus.Info("将覆盖已存在的视频文件: ", outputFile)
		}

		if er := c.Composition(video, audio, outputFile); er != nil {
			logrus.Errorf("%s 合成失败", filepath.Base(outputFile))
			continue
		}
		outputFiles = append(outputFiles, filepath.Join(groupPath, mp4Name))
	}

	// 处理未合并的MP3和视频文件
	if c.Summarize {
		// 查找未合并的MP3和视频文件
		for _, v := range dirs {
			video, audio, e := c.GetAudioAndVideo(v)
			if e != nil {
				continue
			}

			// 检查是否已经合成
			info := filepath.Join(v, conver.VideoInfoJson)
			if !utils.IsExist(info) {
				info = filepath.Join(v, conver.VideoInfoSuffix)
				if !utils.IsExist(info) {
					info = filepath.Join(v, conver.PlayEntryJson)
					if !utils.IsExist(info) {
						continue
					}
				}
			}
			infoStr, e := os.ReadFile(info)
			if e != nil {
				continue
			}
			js, e := simplejson.NewJson(infoStr)
			if e != nil {
				continue
			}

			groupTitle := Filter(js.Get("groupTitle").String())
			groupTitle = null2Str(groupTitle, Filter(js.Get("owner_name").String()))
			uname := Filter(js.Get("uname").String())
			uname = null2Str(uname, Filter(js.Get("title").String()))
			title := Filter(js.Get("page_data").Get("download_subtitle").String())
			title = null2Str(title, Filter(js.Get("title").String()))

			// 添加空值检查，避免创建空目录名或尝试复制空文件路径
			if groupTitle == "" && uname == "" {
				logrus.Warn("项目信息为空，跳过处理未合并文件: ", v)
				continue
			}

			// 创建项目特定的未合并文件夹
			groupPath := groupTitle + "-" + uname
			groupDir := filepath.Join(c.OutputDir, groupPath)
			if !utils.IsExist(groupDir) {
				if err = os.MkdirAll(groupDir, os.ModePerm); err != nil {
					logrus.Error("创建项目目录失败: ", err)
					continue
				}
			}
			summaryDir := filepath.Join(groupDir, "未合并文件")
			if !utils.IsExist(summaryDir) {
				if err = os.MkdirAll(summaryDir, os.ModePerm); err != nil {
					logrus.Error("创建未合并文件目录失败: ", err)
					continue
				}
			}

			// 复制未合并的视频文件
			if utils.IsExist(video) && title != "" {
				videoDest := filepath.Join(summaryDir, title+"_video"+filepath.Ext(video))
				if !utils.IsExist(videoDest) {
					if err := c.copyFile(video, videoDest); err == nil {
						logrus.Info("已将未合并的视频文件放入汇总目录: ", videoDest)
					}
				} else {
					logrus.Warn("未合并的视频文件已存在，跳过复制: ", videoDest)
				}
			}

			// 复制未合并的音频文件
			if utils.IsExist(audio) && title != "" {
				audioDest := filepath.Join(summaryDir, title+"_audio"+filepath.Ext(audio))
				if !utils.IsExist(audioDest) {
					if err := c.copyFile(audio, audioDest); err == nil {
						logrus.Info("已将未合并的音频文件放入汇总目录: ", audioDest)
					}
				} else {
					logrus.Warn("未合并的音频文件已存在，跳过复制: ", audioDest)
				}
			}
		}
	}

	end := time.Now().Unix()
	logrus.Print("===========================================")
	if skipFilePaths != nil {
		logrus.Print("跳过的目录:\n" + strings.Join(skipFilePaths, "\n"))
	}
	if outputFiles != nil {
		logrus.Printf("# 输出目录:\n%s", color.CyanString(c.OutputDir))
		logrus.Printf("# 合成的文件:\n%s", color.CyanString(strings.Join(outputFiles, "\n")))
		// 打开合成文件目录
		go OpenFolder(c.OutputDir)
	} else {
		logrus.Warn("未合成任何文件！")
	}
	logrus.Print("===========================================")
	logrus.Print("已完成合成任务，耗时: ", end-begin, "秒")
	c.wait()
}

func (c *Config) findMp4Info(fp, sub string) bool {
	if !utils.IsExist(c.GPACPath) {
		return false
	}
	ret, err := exec.Command(c.GPACPath, "-info", fp).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(ret), sub)
}

func null2Str(s string, value string) string {
	if s != "" {
		return s
	}
	return value
}

func (c *Config) wait() {
	fmt.Println("按任意键退出程序")
	_, _ = fmt.Scanln()
	os.Exit(0)
}
