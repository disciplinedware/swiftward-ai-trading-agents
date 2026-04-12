package com.trading;

import com.trading.config.AgentProperties;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.scheduling.annotation.EnableScheduling;

@SpringBootApplication
@EnableConfigurationProperties(AgentProperties.class)
@EnableScheduling
public class TradingAgentApplication {

    public static void main(String[] args) {
        SpringApplication.run(TradingAgentApplication.class, args);
    }
}
