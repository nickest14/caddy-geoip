# caddy-geoip

## Overview
Caddy2 plugin that can check user request Geolocation by IP address using MaxMind database. And can set allow list and block list to restrict the specific IP.
[caddy1 reference](https://github.com/aablinov/caddy-geoip/)

## Missing geolocation data

If there is no geolocation data for an IP address most of the placeholders listed above will be empty. The exceptions are `geoip_country_code`,
`geoip_country_name`, and `geoip_city_name`. If the request originated over
the system loopback interface (e.g., 127.0.0.1) those vars will be set
to `**`, `Loopback`, and `Loopback` respectively. For any other address,
including private addresses such as 192.168.0.1, the values will be `!!`,
`No Country`, and `No City` respectively.

## Examples
Set database path, block and allow list. <br>
if allow_list -> allow_only set to <font color="#660000">true</font>  means only the IPs in allow_list can visit. <br>


```
// only US and 120.100.100.100 ip can visit
geoip /path/to/db/GeoLite2-Country.mmdb {
    block_list {
        country TW
        ip 35.100.100.0 // US IP
    }
    allow_list {
        country US     
        ip 120.100.100.0 // TW IP
        allow_only false
    }
}
```

##### If the IP is banned. It will return 403 forbidden. <br>
![402-forbidden](https://github.com/nickest14/caddy-geoip/blob/master/imgs/403-forbidden.png?raw=true)

Return country code and country name in header:
```
header Country-Code {geoip_country_code}
header Country-Name {geoip_country_name}
```
![header-country](https://github.com/nickest14/caddy-geoip/blob/master/imgs/header-country.png?raw=true)

## Complete caddyfile example
```
{
    order geoip first
}

:9010 {
    geoip /path/to/db/GeoLite2-Country.mmdb {
        block_list {
            country TW
            ip 35.100.100.0 // US IP
        }
        allow_list {
            country US     
            ip 120.100.100.0 // TW IP
            allow_only false
        }
    }
    header Country-Code {geoip_country_code}
    header Country-Name {geoip_country_name}
    file_server
}
```
