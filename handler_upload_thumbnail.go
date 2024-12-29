package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)
	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	videoData, err := cfg.db.GetVideo(videoID)
	headerFile, err := header.Open()
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse header", err)
		return
	}
	defer headerFile.Close()

	headerData, err := io.ReadAll(headerFile)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse header", err)
		return
	}
	contentType := http.DetectContentType(headerData)

	mediaType, _, err := mime.ParseMediaType(contentType)

	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Unable to process file", err)
		return
	}
	contentTypes := strings.Split(mediaType, "/")

	randomData := make([]byte, 32)
	rand.Read(randomData)
	fileName := base64.RawURLEncoding.EncodeToString(randomData)

	outFile, err := os.Create(filepath.Join(cfg.assetsRoot, fileName) + "." + contentTypes[1])
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to save thumbnail", err)
		return
	}
	defer outFile.Close()
	io.Copy(outFile, file)

	thumbUrl := fmt.Sprintf("http://localhost:%s/assets/%s.%s", cfg.port, fileName, contentTypes[1])

	videoData.ThumbnailURL = &thumbUrl
	err = cfg.db.UpdateVideo(videoData)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video", err)
		return
	}
	respondWithJSON(w, http.StatusOK, videoData)
}
