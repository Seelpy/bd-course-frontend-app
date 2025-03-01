import { Author } from "./author";

export type CreateBook = {
  title: string;
  description: string;
};

export type EditBook = {
  id: string;
  title: string;
  description: string;
};

export type DeleteBook = {
  id: string;
};

export type Book = {
  bookId: string;
  cover?: string;
  title: string;
  description: string;
  authors: Author[];
};

export type ListBookResponse = {
  books: Book[];
  countPages: number;
};
