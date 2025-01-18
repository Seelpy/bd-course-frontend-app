package query

import (
	"database/sql"
	"github.com/gofrs/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/mono83/maybe"
	"server/pkg/domain/model"
	"strings"
)

type RatingExtremeType int16

const (
	RAITING_EXTREME_MAX RatingExtremeType = iota
	RAITING_EXTREME_MIN
)

type BookChapterExtremeType int16

const (
	BOOK_CHAPTER_MAX BookChapterExtremeType = iota
	BOOK_CHAPTER_MIN
)

type BookQueryService interface {
	FindByID(bookID model.BookID) (BookOutput, error)
	List(spec ListSpec) ([]BookOutput, error)
	ListByIDs(bookIDs []model.BookID) ([]BookOutput, error)
	CountBook(isPublished bool) (int, error)
}

type ListSpec struct {
	Page               int
	Size               int
	BookTitle          maybe.Maybe[string]
	AuthorIDs          maybe.Maybe[[]model.AuthorID]
	RatingExtreme      maybe.Maybe[RatingExtremeType]
	GenreIDs           maybe.Maybe[[]model.GenreID]
	BookChapterExtreme maybe.Maybe[BookChapterExtremeType]
}

type BookOutput struct {
	BookID        uuid.UUID
	Cover         maybe.Maybe[string]
	AverageRating float64
	Title         string
	Description   string
}

type bookQueryService struct {
	connection *sqlx.DB
}

func NewBookQueryService(connection *sqlx.DB) *bookQueryService {
	return &bookQueryService{connection: connection}
}

func (service *bookQueryService) FindByID(bookID model.BookID) (BookOutput, error) {
	const query = `
		SELECT b.book_id, i.path, b.title, b.description, COALESCE(AVG(br.value), 0) as average_rating
		FROM book b
		LEFT JOIN image i ON b.cover_id = i.image_id
		LEFT JOIN book_rating br ON br.book_id = b.book_id
		WHERE b.book_id = ?
		GROUP BY b.book_id, i.path, b.title, b.description;
	`

	binaryBookID, err := uuid.UUID(bookID).MarshalBinary()
	if err != nil {
		return BookOutput{}, err
	}

	var book sqlxBook
	err = service.connection.Get(&book, query, binaryBookID)
	if err != nil {
		return BookOutput{}, err
	}

	cover := maybe.Nothing[string]()
	if book.Cover.Valid {
		cover = maybe.Just(book.Cover.String)
	}

	return BookOutput{
		BookID:        book.BookID,
		Cover:         cover,
		AverageRating: book.AverageRating,
		Title:         book.Title,
		Description:   book.Description,
	}, nil
}

func (service *bookQueryService) ListByIDs(bookIDs []model.BookID) ([]BookOutput, error) {
	if len(bookIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, 0, len(bookIDs))
	args := make([]interface{}, 0, len(bookIDs))
	for _, id := range bookIDs {
		binaryBookID, err := uuid.UUID(id).MarshalBinary()
		if err != nil {
			return nil, err
		}
		placeholders = append(placeholders, "?")
		args = append(args, binaryBookID)
	}

	query := `
		SELECT b.book_id, i.path, b.title, b.description,  COALESCE(AVG(br.value), 0) as average_rating
		FROM book b
		LEFT JOIN image i ON b.cover_id = i.image_id
		LEFT JOIN book_rating br ON br.book_id = b.book_id
		WHERE b.book_id IN (` + strings.Join(placeholders, ",") + `)
		GROUP BY b.book_id, i.path, b.title, b.description;
	`

	var books []sqlxBook
	err := service.connection.Select(&books, query, args...)
	if err != nil {
		return nil, err
	}

	result := make([]BookOutput, 0)
	for _, sqlBook := range books {
		cover := maybe.Nothing[string]()
		if sqlBook.Cover.Valid {
			cover = maybe.Just(sqlBook.Cover.String)
		}

		result = append(result, BookOutput{
			BookID:        sqlBook.BookID,
			Cover:         cover,
			AverageRating: sqlBook.AverageRating,
			Title:         sqlBook.Title,
			Description:   sqlBook.Description,
		})
	}

	return result, nil
}

func (service *bookQueryService) List(spec ListSpec) ([]BookOutput, error) {
	query := `
		SELECT b.book_id, i.path, b.title, b.description,  COALESCE(AVG(br.value), 0) as average_rating
		FROM book b
		LEFT JOIN image i ON b.cover_id = i.image_id
		LEFT JOIN book_rating br ON br.book_id = b.book_id
		WHERE b.is_publish = 1
	`

	var args []interface{}

	if bookTitle, ok := spec.BookTitle.Get(); ok {
		query += " AND b.title LIKE ?"
		args = append(args, "%"+bookTitle+"%")
	}

	if authorIDs, ok := spec.AuthorIDs.Get(); ok && len(authorIDs) > 0 {
		query += " AND b.book_id IN (SELECT ba.book_id FROM book_author ba WHERE ba.author_id IN ("
		for i, authorID := range authorIDs {
			if i > 0 {
				query += ", "
			}
			query += "?"
			args = append(args, authorID)
		}
		query += "))"
	}

	if ratingExtreme, ok := spec.RatingExtreme.Get(); ok {
		switch ratingExtreme {
		case RAITING_EXTREME_MIN:
			query += " AND b.rating >= ?"
			args = append(args, ratingExtreme)
		case RAITING_EXTREME_MAX:
			query += " AND b.rating <= ?"
			args = append(args, ratingExtreme)
		}
	}

	if genreIDs, ok := spec.GenreIDs.Get(); ok && len(genreIDs) > 0 {
		query += " AND b.book_id IN (SELECT bg.book_id FROM book_genre bg WHERE bg.genre_id IN ("
		for i, genreID := range genreIDs {
			if i > 0 {
				query += ", "
			}
			query += "?"
			args = append(args, genreID)
		}
		query += "))"
	}

	if chapterExtreme, ok := spec.BookChapterExtreme.Get(); ok {
		switch chapterExtreme {
		case BOOK_CHAPTER_MIN:
			query += " AND (SELECT COUNT(*) FROM chapter c WHERE c.book_id = b.book_id) >= ?"
			args = append(args, chapterExtreme)
		case BOOK_CHAPTER_MAX:
			query += " AND (SELECT COUNT(*) FROM chapter c WHERE c.book_id = b.book_id) <= ?"
			args = append(args, chapterExtreme)
		}
	}

	query += " ORDER BY b.title LIMIT ? OFFSET ?"
	query += " GROUP BY b.book_id, i.path, b.title, b.description;"
	offset := (spec.Page - 1) * spec.Size
	args = append(args, spec.Size, offset)

	var sqlxBooks []sqlxBook
	err := service.connection.Select(&sqlxBooks, query, args...)
	if err != nil {
		return nil, err
	}

	bookOutputs := make([]BookOutput, len(sqlxBooks))
	for i, b := range sqlxBooks {
		cover := maybe.Nothing[string]()
		if b.Cover.Valid {
			cover = maybe.Just(b.Cover.String)
		}

		bookOutputs[i] = BookOutput{
			BookID:        b.BookID,
			Cover:         cover,
			AverageRating: b.AverageRating,
			Title:         b.Title,
			Description:   b.Description,
		}
	}

	return bookOutputs, nil
}

func (service *bookQueryService) CountBook(isPublished bool) (int, error) {
	const query = `SELECT COUNT(*) FROM book b WHERE b.is_publish = ?`

	var countBook int
	err := service.connection.Get(&countBook, query, isPublished)
	if err != nil {
		return 0, err
	}

	return countBook, nil
}

type sqlxBook struct {
	BookID        uuid.UUID      `db:"book_id"`
	Cover         sql.NullString `db:"path"`
	AverageRating float64        `db:"average_rating"`
	Title         string         `db:"title"`
	Description   string         `db:"description"`
}
