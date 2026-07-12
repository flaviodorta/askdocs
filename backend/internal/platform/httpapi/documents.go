package httpapi

import (
	"errors"
	"net/http"
	"time"

	"askdocs/backend/internal/document"
)

// maxUploadBytes caps the whole multipart body. Uploads are held in memory or
// a temp file by ParseMultipartForm, never fully trusted before this limit.
const maxUploadBytes = 20 << 20 // 20 MiB

type documentResponse struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toDocumentResponse(d document.Document) documentResponse {
	return documentResponse{
		ID:          d.ID,
		Filename:    d.Filename,
		ContentType: d.ContentType,
		Status:      string(d.Status),
		Error:       d.Error,
		CreatedAt:   d.CreatedAt,
		UpdatedAt:   d.UpdatedAt,
	}
}

func (a *api) handleUploadDocument() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)

		file, header, err := r.FormFile("file")
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "file exceeds the 20 MiB limit")
				return
			}
			writeError(w, http.StatusBadRequest, `multipart field "file" is required`)
			return
		}
		defer file.Close()

		doc, err := a.docs.Upload(r.Context(), header.Filename, header.Header.Get("Content-Type"), file)
		if err != nil {
			if errors.Is(err, document.ErrUnsupportedType) {
				writeError(w, http.StatusUnsupportedMediaType, err.Error())
				return
			}
			a.logger.Error("upload document", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		w.Header().Set("Location", "/documents/"+doc.ID)
		writeJSON(w, http.StatusAccepted, toDocumentResponse(doc))
	}
}

func (a *api) handleListDocuments() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		docs, err := a.docs.List(r.Context())
		if err != nil {
			a.logger.Error("list documents", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		out := make([]documentResponse, 0, len(docs))
		for _, d := range docs {
			out = append(out, toDocumentResponse(d))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (a *api) handleRetryDocument() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		doc, err := a.docs.Retry(r.Context(), r.PathValue("id"))
		if err != nil {
			switch {
			case errors.Is(err, document.ErrNotFound):
				writeError(w, http.StatusNotFound, "document not found")
			case errors.Is(err, document.ErrNotRetryable):
				writeError(w, http.StatusConflict, err.Error())
			default:
				a.logger.Error("retry document", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}
		writeJSON(w, http.StatusOK, toDocumentResponse(doc))
	}
}

func (a *api) handleGetDocument() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		doc, err := a.docs.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			if errors.Is(err, document.ErrNotFound) {
				writeError(w, http.StatusNotFound, "document not found")
				return
			}
			a.logger.Error("get document", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, toDocumentResponse(doc))
	}
}
