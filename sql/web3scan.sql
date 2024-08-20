CREATE TABLE `events` (
	`event` varchar(255) NOT NULL,
	`contract` char(42) NOT NULL,
	`blockNumber` bigint NOT NULL,
	`txHash` char(64) NOT NULL,
	`args` text NOT NULL,
	`created` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
