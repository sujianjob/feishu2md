package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/88250/lute"
	"github.com/Wsine/feishu2md/core"
	"github.com/Wsine/feishu2md/utils"
	"github.com/chyroc/lark"
	"github.com/pkg/errors"
)

type DownloadOpts struct {
	outputDir   string
	dump        bool
	batch       bool
	wiki        bool
	incremental bool // 启用增量下载
	force       bool // 强制重新下载
}

var dlOpts = DownloadOpts{}
var dlConfig core.Config

func downloadDocument(ctx context.Context, client *core.Client, url string, opts *DownloadOpts, cacheManager *core.CacheManager) error {
	// Validate the url to download
	docType, docToken, err := utils.ValidateDocumentURL(url)
	if err != nil {
		return err
	}
	fmt.Println("Captured document token:", docToken)

	// for a wiki page, we need to renew docType and docToken first
	if docType == "wiki" {
		node, err := client.GetWikiNodeInfo(ctx, docToken)
		if err != nil {
			err = fmt.Errorf("GetWikiNodeInfo err: %v for %v", err, url)
		}
		utils.CheckErr(err)
		docType = node.ObjType
		docToken = node.ObjToken
	}
	if docType == "docs" {
		return errors.Errorf(
			`Feishu Docs is no longer supported. ` +
				`Please refer to the Readme/Release for v1_support.`)
	}

	// Process the download
	docx, blocks, err := client.GetDocxContent(ctx, docToken)
	utils.CheckErr(err)

	title := docx.Title
	revisionID := docx.RevisionID

	// 确定输出文件名
	var mdName string
	if dlConfig.Output.TitleAsFilename {
		mdName = fmt.Sprintf("%s.md", utils.SanitizeFileName(title))
	} else {
		mdName = fmt.Sprintf("%s.md", docToken)
	}
	outputPath := filepath.Join(opts.outputDir, mdName)

	// 增量下载逻辑：检查是否需要下载
	if opts.incremental && !opts.force && cacheManager != nil {
		shouldDownload, skipReason := cacheManager.ShouldDownload(
			docToken,
			revisionID,
			outputPath,
		)

		if !shouldDownload {
			fmt.Printf("⊘ 跳过: %s - %s\n", title, skipReason)
			// 即使跳过下载，也要更新缓存（用于建立缓存映射）
			cacheManager.UpdateDocument(
				docToken,
				revisionID,
				title,
				mdName,
				docType,
			)
			return nil
		}
	}

	// 继续执行下载流程
	parser := core.NewParser(dlConfig.Output)

	markdown := parser.ParseDocxContent(docx, blocks)

	if !dlConfig.Output.SkipImgDownload {
		for _, imgToken := range parser.ImgTokens {
			localLink, err := client.DownloadImage(
				ctx, imgToken, filepath.Join(opts.outputDir, dlConfig.Output.ImageDir),
			)
			if err != nil {
				return err
			}
			markdown = strings.Replace(markdown, imgToken, localLink, 1)
		}
	}

	// Format the markdown document
	engine := lute.New(func(l *lute.Lute) {
		l.RenderOptions.AutoSpace = true
	})
	result := engine.FormatStr("md", markdown)

	// Handle the output directory and name
	if _, err := os.Stat(opts.outputDir); os.IsNotExist(err) {
		if err := os.MkdirAll(opts.outputDir, 0o755); err != nil {
			return err
		}
	}

	if dlOpts.dump {
		jsonName := fmt.Sprintf("%s.json", docToken)
		jsonOutputPath := filepath.Join(opts.outputDir, jsonName)
		data := struct {
			Document *lark.DocxDocument `json:"document"`
			Blocks   []*lark.DocxBlock  `json:"blocks"`
		}{
			Document: docx,
			Blocks:   blocks,
		}
		pdata := utils.PrettyPrint(data)

		if err = os.WriteFile(jsonOutputPath, []byte(pdata), 0o644); err != nil {
			return err
		}
		fmt.Printf("Dumped json response to %s\n", jsonOutputPath)
	}

	// Write to markdown file
	if err = os.WriteFile(outputPath, []byte(result), 0o644); err != nil {
		return err
	}
	fmt.Printf("✓ Downloaded markdown file to %s\n", outputPath)

	// 更新缓存
	if cacheManager != nil && (opts.incremental || opts.force) {
		cacheManager.UpdateDocument(
			docToken,
			revisionID,
			title,
			mdName,
			docType,
		)
	}

	return nil
}

func downloadDocuments(ctx context.Context, client *core.Client, url string, cacheManager *core.CacheManager) error {
	// Validate the url to download
	folderToken, err := utils.ValidateFolderURL(url)
	if err != nil {
		return err
	}
	fmt.Println("Captured folder token:", folderToken)

	// Error channel and wait group
	errChan := make(chan error)
	wg := sync.WaitGroup{}

	// Recursively go through the folder and download the documents
	var processFolder func(ctx context.Context, folderPath, folderToken string) error
	processFolder = func(ctx context.Context, folderPath, folderToken string) error {
		files, err := client.GetDriveFolderFileList(ctx, nil, &folderToken)
		if err != nil {
			return err
		}
		opts := DownloadOpts{
			outputDir:   folderPath,
			dump:        dlOpts.dump,
			batch:       false,
			incremental: dlOpts.incremental,
			force:       dlOpts.force,
		}
		for _, file := range files {
			if file.Type == "folder" {
				_folderPath := filepath.Join(folderPath, file.Name)
				if err := processFolder(ctx, _folderPath, file.Token); err != nil {
					return err
				}
			} else if file.Type == "docx" {
				// concurrently download the document
				wg.Add(1)
				go func(_url string) {
					if err := downloadDocument(ctx, client, _url, &opts, cacheManager); err != nil {
						errChan <- err
					}
					wg.Done()
				}(file.URL)
			}
		}
		return nil
	}
	if err := processFolder(ctx, dlOpts.outputDir, folderToken); err != nil {
		return err
	}

	// Wait for all the downloads to finish
	go func() {
		wg.Wait()
		close(errChan)
	}()
	for err := range errChan {
		return err
	}
	return nil
}

func downloadWiki(ctx context.Context, client *core.Client, url string, cacheManager *core.CacheManager) error {
	prefixURL, spaceID, err := utils.ValidateWikiURL(url)
	if err != nil {
		return err
	}

	folderPath, err := client.GetWikiName(ctx, spaceID)
	if err != nil {
		return err
	}
	if folderPath == "" {
		return fmt.Errorf("failed to GetWikiName")
	}

	errChan := make(chan error)

	var maxConcurrency = 10 // Set the maximum concurrency level
	wg := sync.WaitGroup{}
	semaphore := make(chan struct{}, maxConcurrency) // Create a semaphore with the maximum concurrency level

	var downloadWikiNode func(ctx context.Context,
		client *core.Client,
		spaceID string,
		parentPath string,
		parentNodeToken *string) error

	downloadWikiNode = func(ctx context.Context,
		client *core.Client,
		spaceID string,
		folderPath string,
		parentNodeToken *string) error {
		nodes, err := client.GetWikiNodeList(ctx, spaceID, parentNodeToken)
		if err != nil {
			return err
		}
		for _, n := range nodes {
			if n.HasChild {
				_folderPath := filepath.Join(folderPath, n.Title)
				if err := downloadWikiNode(ctx, client,
					spaceID, _folderPath, &n.NodeToken); err != nil {
					return err
				}
			}
			if n.ObjType == "docx" {
				opts := DownloadOpts{
					outputDir:   folderPath,
					dump:        dlOpts.dump,
					batch:       false,
					incremental: dlOpts.incremental,
					force:       dlOpts.force,
				}
				wg.Add(1)
				semaphore <- struct{}{}
				go func(_url string) {
					if err := downloadDocument(ctx, client, _url, &opts, cacheManager); err != nil {
						errChan <- err
					}
					wg.Done()
					<-semaphore
				}(prefixURL + "/wiki/" + n.NodeToken)
			}
		}
		return nil
	}

	if err = downloadWikiNode(ctx, client, spaceID, folderPath, nil); err != nil {
		return err
	}

	// Wait for all the downloads to finish
	go func() {
		wg.Wait()
		close(errChan)
	}()
	for err := range errChan {
		return err
	}
	return nil
}

func handleDownloadCommand(url string) error {
	// Load config
	configPath, err := core.GetConfigFilePath()
	if err != nil {
		return err
	}
	config, err := core.ReadConfigFromFile(configPath)
	if err != nil {
		return err
	}
	dlConfig = *config

	// Instantiate the client
	client := core.NewClient(dlConfig.Feishu)
	ctx := context.Background()

	// 初始化缓存管理器
	var cacheManager *core.CacheManager
	if dlOpts.incremental || dlOpts.force {
		cacheManager, err = core.NewCacheManager(dlOpts.outputDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "警告: 无法初始化缓存管理器: %v\n", err)
			cacheManager = nil
		}

		if dlOpts.force && cacheManager != nil {
			fmt.Println("强制模式: 将重新下载所有文档并更新缓存")
		} else if dlOpts.incremental && cacheManager != nil {
			fmt.Println("增量模式: 将跳过未修改的文档")
		}
	}

	// 执行下载
	var downloadErr error
	if dlOpts.batch {
		downloadErr = downloadDocuments(ctx, client, url, cacheManager)
	} else if dlOpts.wiki {
		downloadErr = downloadWiki(ctx, client, url, cacheManager)
	} else {
		downloadErr = downloadDocument(ctx, client, url, &dlOpts, cacheManager)
	}

	// 保存缓存
	if cacheManager != nil {
		if err := cacheManager.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "警告: 缓存保存失败: %v\n", err)
		} else if dlOpts.incremental || dlOpts.force {
			fmt.Println("✓ 缓存已更新")
		}
	}

	return downloadErr
}
