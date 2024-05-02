package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

var HOST = flag.String("host", "0.0.0.0", "run server host")
var PORT = flag.Int("port", 17888, "run server port")
var API_BASE = flag.String("api_base", "https://api.openai.com", "openai api base url")
var MODEL_TABLE_FILE = flag.String("model_table", "model_table.json", "model table file")
var MODEL_TABLE map[string]string

var DEFAULT_READMD = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>OpenAI Model Forward</title>
</head>
<body>
    <h1>OpenAI Model Forward</h1>
    <p>see <a href="https://github.com/nerdneilsfield/openapi-model-replace">GitHub Repo</a></p>
</body>
</html>
`

//go:embed github-markdown.css README.md index.html
var f embed.FS

type PageData struct {
	MarkdownContent template.HTML
}

// OpenAIRequest represents a request to the OpenAI API.
type OpenAIRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Stream bool `json:"stream"`
}

// load file in json and save into table
func LoadModelTable(filepath string) {
	// read file
	file, err := os.Open(*MODEL_TABLE_FILE)
	if err != nil {
		log.Fatal("Read file error ", err.Error())
		panic(err)
	}
	byteValue, err := io.ReadAll(file)
	if err != nil {
		log.Fatal("Read file error ", err.Error())
		panic(err)
	}

	err = json.Unmarshal(byteValue, &MODEL_TABLE)
	if err != nil {
		log.Fatalln("Unmarshal json error ", err.Error())
	}
}

func LoadReadMe() []byte {
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs | parser.NoEmptyLineBeforeBlock
	p := parser.NewWithExtensions(extensions)
	htmlFlags := html.CommonFlags | html.HrefTargetBlank
	opts := html.RendererOptions{Flags: htmlFlags}
	renderer := html.NewRenderer(opts)

	content, err := f.ReadFile("README.md")
	if err != nil {
		return []byte(DEFAULT_READMD)
	}

	doc := p.Parse(content)
	htmlData := markdown.Render(doc, renderer)

	tempContent, err := f.ReadFile("index.html")
	if err != nil {
		return []byte(DEFAULT_READMD)
	}

	tmpl, err := template.New("webpage").Parse(string(tempContent))
	if err != nil {
		return []byte(DEFAULT_READMD)
	}

	pageData := PageData{
		MarkdownContent: template.HTML(htmlData),
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, pageData) // Pass a pointer to buf
	if err != nil {
		return []byte(DEFAULT_READMD)
	}

	return buf.Bytes()
}

func copyHeaders(src http.Header, dest http.Header) {
	for key, values := range src {
		for _, value := range values {
			dest.Add(key, value)
		}
	}
}

func main() {

	flag.Parse()

	LoadModelTable(*MODEL_TABLE_FILE)

	gin.SetMode(gin.ReleaseMode)

	r := gin.Default()

	r.GET("/", func(c *gin.Context) {
		helpHtml := LoadReadMe()
		c.Data(http.StatusOK, "text/html", helpHtml)
	})

	r.GET("/css/github-markdown.css", func(c *gin.Context) {
		content, err := f.ReadFile("github-markdown.css")
		if err != nil {
			log.Println("Failed to read github-markdown.css")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read github-markdown.css", "code": http.StatusInternalServerError})
		}
		c.Data(http.StatusOK, "text/css", content)
	})

	r.POST("/v1/chat/completions", func(c *gin.Context) {
		var openAIreq OpenAIRequest

		if err := c.ShouldBind(&openAIreq); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": http.StatusBadRequest})
		}

		// if model in model_table keys replace it
		if _, ok := MODEL_TABLE[openAIreq.Model]; ok {
			originModedl := openAIreq.Model
			replacedModel := MODEL_TABLE[originModedl]
			log.Println("Replace model from ", originModedl, " to ", replacedModel)
			openAIreq.Model = MODEL_TABLE[openAIreq.Model]
		}

		// 将修改后的数据转换为JSON
		modifiedData, err := json.Marshal(openAIreq)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal modified data"})
			return
		}

		// 创建新的HTTP客户端请求
		req, err := http.NewRequest("POST", *API_BASE+"/v1/chat/completions", bytes.NewBuffer(modifiedData))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
			return
		}
		copyHeaders(c.Request.Header, req.Header)

		// 发送请求
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to forward request"})
			return
		}
		defer resp.Body.Close()

		// read the resp body and print
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response body"})
			return
		}
		// log.Println("Response body: ", string(respBody))

		// copy resp data to gin response, include headers
		for key, values := range resp.Header {
			for _, value := range values {
				c.Header(key, value)
			}
		}

		// 将响应返回给原始请求者
		c.Data(resp.StatusCode, "application/json", respBody)
	})
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Not found", "code": http.StatusNotFound})
	})

	log.Println("Server is running on " + *HOST + ":" + strconv.Itoa(*PORT) + " ...")
	r.Run(*HOST + ":" + strconv.Itoa(*PORT))
}
