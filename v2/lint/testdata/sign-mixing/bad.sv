module top (input signed [7:0] s, input [7:0] u, input int i, input logic [3:0] n, output logic o, output logic [7:0] sum);
  assign o = s < u;                    // want
  assign sum = s + u;                  // want
  assign o = i == n;                   // want
endmodule
