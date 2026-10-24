module top #(parameter W = 8, parameter SPARE_ONE = 1) (input [W-1:0] d); // want
  localparam DEPTH = 4;             // want
  localparam USED = W * 2;
  logic [USED-1:0] v = d;
endmodule
