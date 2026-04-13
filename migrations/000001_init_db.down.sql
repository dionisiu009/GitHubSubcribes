-- Відкатуємо у зворотному порядку залежностей
DROP VIEW  IF EXISTS active_subscribers_view;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS subscribers;
DROP TABLE IF EXISTS repositories;
