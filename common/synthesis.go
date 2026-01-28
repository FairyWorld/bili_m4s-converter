package common

import (
	"fmt"
	"m4s-converter/conver"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
		// 生成输出文件名
		mp4Name := title + conver.Mp4Suffix
		outputFile := filepath.Join(groupDir, mp4Name)

		// 设置元数据信息
		c.Title = title
		c.Uname = uname
		c.GroupTitle = groupTitle

		// 检查目录中是否存在与输入音频和视频文件内容相同的文件
		if exists, existingFile := c.isIdenticalFileExists(groupDir, video, audio); exists {
			logrus.Warn("跳过完全相同的视频: ", existingFile)
			continue
		}

		// 检查是否已经存在同名文件
		if utils.IsExist(outputFile) {
			logrus.Warn("跳过同名视频: ", outputFile)
			continue
		}

		// 执行合成
		if er := c.Composition(video, audio, outputFile); er != nil {
			logrus.Errorf("%s 合成失败", filepath.Base(outputFile))
			continue
		}

		// 生成并存储文件哈希值，用于后续的重复检测
		hashFile := strings.ReplaceAll(outputFile, ".mp4", ".hash")
		inputHash := c.calculateCombinedHash(video, audio)
		if inputHash != "" {
			_ = os.WriteFile(hashFile, []byte(inputHash), 0644)
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

// getMp4KeyInfo 从MP4文件中提取关键信息
func (c *Config) getMp4KeyInfo(fp string) (map[string]string, error) {
	if !utils.IsExist(c.GPACPath) {
		return nil, fmt.Errorf("GPAC路径不存在")
	}

	ret, err := exec.Command(c.GPACPath, "-info", fp).CombinedOutput()
	if err != nil {
		return nil, err
	}

	info := string(ret)
	keyInfo := make(map[string]string)

	// 提取视频信息
	if videoMatch := regexp.MustCompile(`Video #\d+: (.*?) \(.*?\)`).FindStringSubmatch(info); len(videoMatch) > 1 {
		keyInfo["video_codec"] = videoMatch[1]
	}

	if resolutionMatch := regexp.MustCompile(`Resolution: (\d+x\d+)`).FindStringSubmatch(info); len(resolutionMatch) > 1 {
		keyInfo["resolution"] = resolutionMatch[1]
	}

	if frameRateMatch := regexp.MustCompile(`Frame rate: ([\d.]+) fps`).FindStringSubmatch(info); len(frameRateMatch) > 1 {
		keyInfo["frame_rate"] = frameRateMatch[1]
	}

	// 提取音频信息
	if audioMatch := regexp.MustCompile(`Audio #\d+: (.*?) \(.*?\)`).FindStringSubmatch(info); len(audioMatch) > 1 {
		keyInfo["audio_codec"] = audioMatch[1]
	}

	if sampleRateMatch := regexp.MustCompile(`Sample rate: (\d+) Hz`).FindStringSubmatch(info); len(sampleRateMatch) > 1 {
		keyInfo["sample_rate"] = sampleRateMatch[1]
	}

	return keyInfo, nil
}

// compareMp4Info 比较两个MP4文件的关键信息是否相同
func (c *Config) compareMp4Info(file1, file2 string) bool {
	info1, err1 := c.getMp4KeyInfo(file1)
	info2, err2 := c.getMp4KeyInfo(file2)

	if err1 != nil || err2 != nil {
		logrus.Warnf("获取MP4信息失败: %v, %v", err1, err2)
		return false
	}

	// 比较关键信息
	keyFields := []string{"video_codec", "resolution", "frame_rate", "audio_codec", "sample_rate"}
	for _, key := range keyFields {
		if info1[key] != info2[key] {
			logrus.Infof("文件信息不同: %s: %s vs %s", key, info1[key], info2[key])
			return false
		}
	}

	return true
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
