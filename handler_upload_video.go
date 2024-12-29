package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func processVideoForFastStart(filePath string) (string, error) {
	outpath := fmt.Sprintf("%s.processing", filePath)
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outpath)
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return outpath, nil
}

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	const maxMemory = 10 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxMemory)

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

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to process this request", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized to edit this content", err)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

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
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Unable to process file", err)
		return
	}

	tmp, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to process upload", err)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	_, err = io.Copy(tmp, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to process upload", err)
		return
	}

	ratio, err := getVideoAspect(tmp.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to process upload", err)
		return
	}

	fastFilePath, err := processVideoForFastStart(tmp.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to convert upload", err)
		return
	}
	fastFile, err := os.Open(fastFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to process upload", err)
		return
	}
	defer os.Remove(fastFile.Name())
	defer fastFile.Close()

	randomData := make([]byte, 32)
	rand.Read(randomData)
	fileName := fmt.Sprintf("%s/%s", ratio, base64.RawURLEncoding.EncodeToString(randomData))
	log.Printf("Uploading video %s with name %s by user %s", videoID, fileName, userID)
	_, err = cfg.s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileName,
		Body:        fastFile,
		ContentType: &mediaType,
	})

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to store file", err)
		return
	}
	videoUrl := fmt.Sprintf("%s/%s", cfg.s3CfDistribution, fileName)
	video.VideoURL = &videoUrl
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
